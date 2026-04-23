package service

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog"

	"github.com/blesta/wa-reminder/internal/config"
	"github.com/blesta/wa-reminder/internal/domain/model"
	"github.com/blesta/wa-reminder/internal/repository/postgres"
)

var phoneRegex = regexp.MustCompile(`^62[0-9]{8,15}$`)

type ReminderService struct {
	repo            *postgres.Repository
	runtimeSettings *RuntimeSettingsService
	cfg             *config.Config
	logger          zerolog.Logger
	validate        *validator.Validate
}

func NewReminderService(repo *postgres.Repository, runtimeSettings *RuntimeSettingsService, cfg *config.Config, logger zerolog.Logger) *ReminderService {
	return &ReminderService{
		repo:            repo,
		runtimeSettings: runtimeSettings,
		cfg:             cfg,
		logger:          logger,
		validate:        validator.New(),
	}
}

func (s *ReminderService) CreateReminder(ctx context.Context, req model.CreateReminderRequest, idempotencyKey string) (*model.ReminderJob, error) {
	if err := s.validate.Struct(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	if !phoneRegex.MatchString(req.Phone) {
		return nil, fmt.Errorf("phone is invalid")
	}

	settings, err := s.runtimeSettings.Get(ctx)
	if err != nil {
		return nil, err
	}

	if req.SendAt == nil {
		now := time.Now().UTC()
		req.SendAt = &now
	}

	return s.repo.CreateReminder(ctx, 1, req, idempotencyKey, settings.MaxAttempts)
}

func (s *ReminderService) CreateBulkReminder(ctx context.Context, req model.CreateBulkReminderRequest, idempotencyPrefix string) ([]map[string]interface{}, error) {
	if err := s.validate.Struct(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	results := make([]map[string]interface{}, 0, len(req.Items))
	for i, item := range req.Items {
		idempotency := idempotencyPrefix
		if idempotency != "" {
			idempotency = fmt.Sprintf("%s-%d", idempotencyPrefix, i)
		}
		job, err := s.CreateReminder(ctx, item, idempotency)
		if err != nil {
			results = append(results, map[string]interface{}{
				"index":       i,
				"external_id": item.ExternalID,
				"error": map[string]string{
					"code":    "VALIDATION_ERROR",
					"message": err.Error(),
				},
			})
			continue
		}
		results = append(results, map[string]interface{}{
			"index":       i,
			"external_id": item.ExternalID,
			"job_id":      job.JobUUID,
			"status":      job.Status,
		})
	}
	return results, nil
}

func (s *ReminderService) GetReminder(ctx context.Context, jobUUID string) (*model.ReminderJob, error) {
	return s.repo.GetReminderByJobUUID(ctx, jobUUID)
}

func (s *ReminderService) CancelReminder(ctx context.Context, jobUUID string) error {
	return s.repo.CancelReminder(ctx, jobUUID)
}
