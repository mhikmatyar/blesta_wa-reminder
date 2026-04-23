package service

import (
	"context"
	"encoding/json"

	"github.com/blesta/wa-reminder/internal/config"
	"github.com/blesta/wa-reminder/internal/domain/model"
	"github.com/blesta/wa-reminder/internal/repository/postgres"
)

type RuntimeSettingsService struct {
	repo *postgres.Repository
	cfg  *config.Config
}

func NewRuntimeSettingsService(repo *postgres.Repository, cfg *config.Config) *RuntimeSettingsService {
	return &RuntimeSettingsService{repo: repo, cfg: cfg}
}

func (s *RuntimeSettingsService) Get(ctx context.Context) (model.QueueRuntimeSettings, error) {
	settings := model.QueueRuntimeSettings{
		TypingDurationMS: s.cfg.DefaultTypingDurationMS,
		DelayMinSeconds:  s.cfg.DefaultDelayMinSec,
		DelayMaxSeconds:  s.cfg.DefaultDelayMaxSec,
		RetryBackoffSec:  s.cfg.DefaultRetryBackoffSec,
		MaxAttempts:      s.cfg.DefaultMaxAttempts,
		DailyCap:         s.cfg.DefaultDailyCap,
		HourlyCap:        s.cfg.DefaultHourlyCap,
		SendWindowStart:  s.cfg.DefaultSendWindowStart,
		SendWindowEnd:    s.cfg.DefaultSendWindowEnd,
		QueuePaused:      false,
	}

	raw, err := s.repo.GetAppSettings(ctx)
	if err != nil {
		return settings, err
	}

	overrideInt(raw, "typing_duration_ms", &settings.TypingDurationMS)
	overrideInt(raw, "delay_min_seconds", &settings.DelayMinSeconds)
	overrideInt(raw, "delay_max_seconds", &settings.DelayMaxSeconds)
	overrideInt(raw, "max_attempts", &settings.MaxAttempts)
	overrideInt(raw, "daily_cap", &settings.DailyCap)
	overrideInt(raw, "hourly_cap", &settings.HourlyCap)
	overrideString(raw, "send_window_start", &settings.SendWindowStart)
	overrideString(raw, "send_window_end", &settings.SendWindowEnd)
	overrideBool(raw, "queue_paused", &settings.QueuePaused)
	overrideIntSlice(raw, "retry_backoff_seconds", &settings.RetryBackoffSec)

	if settings.DelayMinSeconds > settings.DelayMaxSeconds {
		settings.DelayMinSeconds, settings.DelayMaxSeconds = settings.DelayMaxSeconds, settings.DelayMinSeconds
	}
	return settings, nil
}

func (s *RuntimeSettingsService) PauseQueue(ctx context.Context) error {
	return s.repo.SetAppSetting(ctx, "queue_paused", true)
}

func (s *RuntimeSettingsService) ResumeQueue(ctx context.Context) error {
	return s.repo.SetAppSetting(ctx, "queue_paused", false)
}

func overrideInt(raw map[string]json.RawMessage, key string, target *int) {
	val, ok := raw[key]
	if !ok {
		return
	}
	var out int
	if err := json.Unmarshal(val, &out); err == nil {
		*target = out
	}
}

func overrideBool(raw map[string]json.RawMessage, key string, target *bool) {
	val, ok := raw[key]
	if !ok {
		return
	}
	var out bool
	if err := json.Unmarshal(val, &out); err == nil {
		*target = out
	}
}

func overrideString(raw map[string]json.RawMessage, key string, target *string) {
	val, ok := raw[key]
	if !ok {
		return
	}
	var out string
	if err := json.Unmarshal(val, &out); err == nil && out != "" {
		*target = out
	}
}

func overrideIntSlice(raw map[string]json.RawMessage, key string, target *[]int) {
	val, ok := raw[key]
	if !ok {
		return
	}
	var out []int
	if err := json.Unmarshal(val, &out); err == nil && len(out) > 0 {
		*target = out
	}
}
