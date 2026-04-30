package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/blesta/wa-reminder/internal/repository/postgres"
)

var reminderTemplateLabels = map[string]string{
	"expiry_h30": "30 hari",
	"expiry_h15": "15 hari",
	"expiry_h7":  "7 hari",
}

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

func (s *AdminService) RefreshWAQR(ctx context.Context) error {
	return s.waService.RefreshQR(ctx)
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
	return s.ListDeliveriesAdvanced(ctx, status, "", "", "", page, limit)
}

func (s *AdminService) ListDeliveriesAdvanced(ctx context.Context, status, search, fromRaw, toRaw string, page, limit int) (map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var from, to *time.Time
	if fromRaw != "" {
		f := parseDateOrDatetime(fromRaw)
		if f == nil {
			return nil, fmt.Errorf("invalid from date")
		}
		from = f
	}
	if toRaw != "" {
		t := parseDateOrDatetime(toRaw)
		if t == nil {
			return nil, fmt.Errorf("invalid to date")
		}
		to = t
	}
	if from != nil && to != nil && from.After(*to) {
		return nil, fmt.Errorf("from cannot be greater than to")
	}
	offset := (page - 1) * limit
	items, err := s.repo.ListDeliveriesFiltered(ctx, status, search, from, to, limit, offset)
	if err != nil {
		return nil, err
	}
	total, err := s.repo.CountDeliveriesFiltered(ctx, status, search, from, to)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"items": items,
		"meta": map[string]interface{}{
			"page":   page,
			"limit":  limit,
			"total":  total,
			"status": status,
			"search": search,
			"from":   fromRaw,
			"to":     toRaw,
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

func (s *AdminService) GetReminderTemplates(ctx context.Context) ([]map[string]interface{}, error) {
	items, err := s.repo.ListReminderTemplates(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}{
			"template_code":    item.TemplateCode,
			"label":            reminderTemplateLabels[item.TemplateCode],
			"message_template": item.MessageTemplate,
			"updated_at":       item.UpdatedAt,
		})
	}
	return out, nil
}

func (s *AdminService) UpdateReminderTemplate(ctx context.Context, templateCode, messageTemplate string) error {
	templateCode = strings.TrimSpace(templateCode)
	messageTemplate = strings.TrimSpace(messageTemplate)
	if templateCode == "" {
		return fmt.Errorf("template_code is required")
	}
	if messageTemplate == "" {
		return fmt.Errorf("message_template is required")
	}
	return s.repo.UpsertReminderTemplate(ctx, templateCode, messageTemplate)
}

func (s *AdminService) PauseQueue(ctx context.Context) error {
	return s.runtimeSettings.PauseQueue(ctx)
}

func (s *AdminService) ResumeQueue(ctx context.Context) error {
	return s.runtimeSettings.ResumeQueue(ctx)
}

func (s *AdminService) ExportDeliveriesCSV(ctx context.Context, status, search, fromRaw, toRaw string) ([]byte, string, error) {
	var from, to *time.Time
	if fromRaw != "" {
		from = parseDateOrDatetime(fromRaw)
		if from == nil {
			return nil, "", fmt.Errorf("invalid from date")
		}
	}
	if toRaw != "" {
		to = parseDateOrDatetime(toRaw)
		if to == nil {
			return nil, "", fmt.Errorf("invalid to date")
		}
	}
	if from != nil && to != nil && from.After(*to) {
		return nil, "", fmt.Errorf("from cannot be greater than to")
	}
	data, err := s.repo.ExportDeliveriesCSV(ctx, status, search, from, to)
	if err != nil {
		return nil, "", err
	}
	values := url.Values{}
	if status != "" {
		values.Set("status", status)
	}
	if search != "" {
		values.Set("search", search)
	}
	if fromRaw != "" {
		values.Set("from", fromRaw)
	}
	if toRaw != "" {
		values.Set("to", toRaw)
	}
	filename := "deliveries_" + time.Now().Format("20060102_150405")
	if values.Encode() != "" {
		filename += "_" + values.Encode()
	}
	filename += ".csv"
	return data, filename, nil
}

func parseDateOrDatetime(raw string) *time.Time {
	layouts := []string{time.RFC3339, "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.Parse(l, raw); err == nil {
			if l == "2006-01-02" {
				v := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
				return &v
			}
			u := t.UTC()
			return &u
		}
	}
	return nil
}
