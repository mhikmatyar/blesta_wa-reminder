package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/blesta/wa-reminder/internal/repository/postgres"
)

type AdminService struct {
	repo            *postgres.Repository
	runtimeSettings *RuntimeSettingsService
	waService       *WAService
	logger          zerolog.Logger
}

func NewAdminService(repo *postgres.Repository, runtimeSettings *RuntimeSettingsService, waService *WAService, logger zerolog.Logger) *AdminService {
	return &AdminService{
		repo:            repo,
		runtimeSettings: runtimeSettings,
		waService:       waService,
		logger:          logger,
	}
}

func (s *AdminService) GetWAStatus(ctx context.Context) (interface{}, error) {
	return s.waService.GetStatus(ctx)
}

func (s *AdminService) GetWAQR(ctx context.Context) (interface{}, error) {
	return s.waService.GetQR(ctx)
}

func (s *AdminService) ReconnectWA(ctx context.Context) error {
	return s.waService.Reconnect(ctx)
}

func (s *AdminService) LogoutWA(ctx context.Context) error {
	return s.waService.Logout(ctx)
}

func (s *AdminService) GetStatsOverview(ctx context.Context, rng string) (map[string]interface{}, error) {
	since := time.Now().Add(-24 * time.Hour)
	switch rng {
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		since = time.Now().Add(-30 * 24 * time.Hour)
	}
	stats, err := s.repo.GetStatsOverview(ctx, since)
	if err != nil {
		return nil, err
	}
	sent := float64(stats["sent"])
	failed := float64(stats["failed"])
	successRate := 0.0
	if sent+failed > 0 {
		successRate = (sent / (sent + failed)) * 100
	}
	return map[string]interface{}{
		"range":        rng,
		"queued":       stats["queued"],
		"processing":   stats["processing"],
		"sent":         stats["sent"],
		"retrying":     stats["retrying"],
		"failed":       stats["failed"],
		"success_rate": successRate,
	}, nil
}

func (s *AdminService) ListDeliveries(ctx context.Context, status string, page, limit int) (map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit
	items, err := s.repo.ListDeliveries(ctx, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"items": items,
		"meta": map[string]interface{}{
			"page":  page,
			"limit": limit,
		},
	}, nil
}

func (s *AdminService) GetDelivery(ctx context.Context, id string) (interface{}, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid delivery id")
	}
	return s.repo.GetDeliveryByID(ctx, n)
}

func (s *AdminService) PauseQueue(ctx context.Context) error {
	return s.runtimeSettings.PauseQueue(ctx)
}

func (s *AdminService) ResumeQueue(ctx context.Context) error {
	return s.runtimeSettings.ResumeQueue(ctx)
}
