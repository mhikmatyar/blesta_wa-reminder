package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/rs/zerolog"
	waTypes "go.mau.fi/whatsmeow/types"

	"github.com/blesta/wa-reminder/internal/config"
	"github.com/blesta/wa-reminder/internal/domain/model"
	"github.com/blesta/wa-reminder/internal/repository/postgres"
)

type WorkerService struct {
	repo            *postgres.Repository
	waService       *WAService
	runtimeSettings *RuntimeSettingsService
	cfg             *config.Config
	logger          zerolog.Logger
}

func NewWorkerService(repo *postgres.Repository, waService *WAService, runtimeSettings *RuntimeSettingsService, cfg *config.Config, logger zerolog.Logger) *WorkerService {
	return &WorkerService{
		repo:            repo,
		waService:       waService,
		runtimeSettings: runtimeSettings,
		cfg:             cfg,
		logger:          logger,
	}
}

func (s *WorkerService) Start(ctx context.Context, pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil {
				s.logger.Error().Err(err).Msg("worker cycle failed")
			}
		}
	}
}

func (s *WorkerService) runOnce(ctx context.Context) error {
	settings, err := s.runtimeSettings.Get(ctx)
	if err != nil {
		return err
	}
	if settings.QueuePaused {
		return nil
	}
	waStatus, err := s.repo.GetWAStatus(ctx)
	if err != nil {
		return err
	}
	if !shouldProcessForWAStatus(waStatus.ConnectionStatus) {
		s.logger.Info().Str("wa_status", string(waStatus.ConnectionStatus)).Msg("worker skip cycle: wa not connected")
		return nil
	}

	jobs, err := s.repo.PickDueJobs(ctx, s.cfg.WorkerBatchSize, s.cfg.WorkerID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := s.processJob(ctx, job, settings); err != nil {
			s.logger.Error().Err(err).Str("job_id", job.JobUUID).Msg("failed processing job")
		}

		delay := randomDelay(settings.DelayMinSeconds, settings.DelayMaxSeconds)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
	}
	return nil
}

func (s *WorkerService) processJob(ctx context.Context, job model.ReminderJob, settings model.QueueRuntimeSettings) error {
	if !isValidPhone(job.PhoneE164) {
		if err := s.repo.MarkJobFailed(ctx, job, "INVALID_PHONE_FORMAT", "phone invalid format", 0, true); err != nil {
			return err
		}
		return s.repo.UpsertUnreachable(ctx, job.PhoneE164, nil, "INVALID_PHONE_FORMAT", "phone invalid format", s.cfg.UnreachableRecheckDays, &job.ID)
	}

	blocked, err := s.repo.IsPhoneBlocked(ctx, job.PhoneE164)
	if err != nil {
		return err
	}
	if blocked {
		return s.repo.MarkJobFailed(ctx, job, "PHONE_BLOCKED", "phone blocked by unreachable registry", 0, true)
	}

	var canonicalJID *string
	cache, err := s.repo.GetNumberCache(ctx, job.PhoneE164)
	if err == nil {
		if !cache.IsOnWhatsApp {
			if err := s.repo.MarkJobFailed(ctx, job, "NOT_ON_WHATSAPP", "phone not registered on whatsapp", 0, true); err != nil {
				return err
			}
			return s.repo.UpsertUnreachable(ctx, job.PhoneE164, cache.CanonicalJID, "NOT_ON_WHATSAPP", "phone not registered on whatsapp", s.cfg.UnreachableRecheckDays, &job.ID)
		}
		canonicalJID = cache.CanonicalJID
	} else if !errors.Is(err, postgres.ErrNotFound) {
		return err
	}

	if canonicalJID == nil {
		ok, canonical, checkErr := s.waService.CheckPhoneOnWhatsApp(ctx, job.PhoneE164)
		if checkErr != nil {
			return s.repo.MarkJobFailed(ctx, job, "WA_NOT_CONNECTED", checkErr.Error(), pickRetry(settings, job.AttemptCount), false)
		}
		_ = s.repo.UpsertNumberCache(ctx, job.PhoneE164, canonical, ok, s.cfg.NumberCheckCacheTTLHours)
		_ = s.repo.SetWhatsAppCheckResult(ctx, job.ID, ok, canonical)

		if !ok {
			if err := s.repo.MarkJobFailed(ctx, job, "NOT_ON_WHATSAPP", "phone not registered on whatsapp", 0, true); err != nil {
				return err
			}
			return s.repo.UpsertUnreachable(ctx, job.PhoneE164, canonical, "NOT_ON_WHATSAPP", "phone not registered on whatsapp", s.cfg.UnreachableRecheckDays, &job.ID)
		}
		canonicalJID = canonical
	}

	if canonicalJID == nil {
		return s.repo.MarkJobFailed(ctx, job, "WA_JID_INVALID", "canonical JID empty", 0, true)
	}

	jid, err := waTypes.ParseJID(*canonicalJID)
	if err != nil {
		if markErr := s.repo.MarkJobFailed(ctx, job, "WA_JID_INVALID", err.Error(), 0, true); markErr != nil {
			return markErr
		}
		return s.repo.UpsertUnreachable(ctx, job.PhoneE164, canonicalJID, "WA_JID_INVALID", err.Error(), s.cfg.UnreachableRecheckDays, &job.ID)
	}

	message := s.buildMessage(ctx, job)
	sendCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	msgID, err := s.waService.SendTextWithTyping(sendCtx, jid, message, time.Duration(settings.TypingDurationMS)*time.Millisecond)
	if err != nil {
		errCode := "WA_SEND_TIMEOUT"
		if !strings.Contains(strings.ToLower(err.Error()), "timeout") {
			errCode = "WA_SEND_ERROR"
		}
		return s.repo.MarkJobFailed(ctx, job, errCode, err.Error(), pickRetry(settings, job.AttemptCount), false)
	}

	return s.repo.MarkJobSent(ctx, job.ID, msgID, settings.TypingDurationMS)
}

func (s *WorkerService) buildMessage(ctx context.Context, job model.ReminderJob) string {
	template, err := s.repo.GetReminderTemplateByCode(ctx, job.TemplateCode)
	if err != nil {
		return buildMessageFallback(job)
	}
	rendered := renderMessageTemplate(template.MessageTemplate, job, s.cfg.AppTimeZone)
	if strings.TrimSpace(rendered) == "" {
		return buildMessageFallback(job)
	}
	return rendered
}

func pickRetry(settings model.QueueRuntimeSettings, attemptCount int) int {
	if len(settings.RetryBackoffSec) == 0 {
		return 300
	}
	idx := attemptCount
	if idx < 0 {
		idx = 0
	}
	if idx >= len(settings.RetryBackoffSec) {
		return settings.RetryBackoffSec[len(settings.RetryBackoffSec)-1]
	}
	return settings.RetryBackoffSec[idx]
}

func randomDelay(minSec, maxSec int) time.Duration {
	if minSec < 0 {
		minSec = 0
	}
	if maxSec < minSec {
		maxSec = minSec
	}
	if maxSec == minSec {
		return time.Duration(minSec) * time.Second
	}
	return time.Duration(minSec+rand.Intn(maxSec-minSec+1)) * time.Second
}

func isValidPhone(phone string) bool {
	if len(phone) < 10 || len(phone) > 18 {
		return false
	}
	if !strings.HasPrefix(phone, "62") {
		return false
	}
	for _, r := range phone {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func shouldProcessForWAStatus(status model.WAConnectionStatus) bool {
	return status == model.WAStatusConnected
}

func buildMessageFallback(job model.ReminderJob) string {
	customerName := "Pelanggan"
	if job.CustomerName != nil && *job.CustomerName != "" {
		customerName = *job.CustomerName
	}
	serviceName := "layanan"
	if job.ServiceName != nil && *job.ServiceName != "" {
		serviceName = *job.ServiceName
	}
	return fmt.Sprintf("Halo %s, ini pengingat untuk layanan %s. Mohon cek masa aktif layanan Anda. Terima kasih.", customerName, serviceName)
}

func renderMessageTemplate(messageTemplate string, job model.ReminderJob, timeZone string) string {
	customerName := "Pelanggan"
	if job.CustomerName != nil && strings.TrimSpace(*job.CustomerName) != "" {
		customerName = strings.TrimSpace(*job.CustomerName)
	}

	serviceName := "layanan"
	if job.ServiceName != nil && strings.TrimSpace(*job.ServiceName) != "" {
		serviceName = strings.TrimSpace(*job.ServiceName)
	}

	expiredDate := "-"
	if job.ExpiredAt != nil {
		loc, err := time.LoadLocation(timeZone)
		if err != nil {
			loc = time.FixedZone("WIB", 7*60*60)
		}
		expiredDate = job.ExpiredAt.In(loc).Format("02/01/2006")
	}

	rendered := messageTemplate
	rendered = strings.ReplaceAll(rendered, "{{customer_name}}", customerName)
	rendered = strings.ReplaceAll(rendered, "{{service_name}}", serviceName)
	rendered = strings.ReplaceAll(rendered, "{{expired_date}}", expiredDate)
	return rendered
}
