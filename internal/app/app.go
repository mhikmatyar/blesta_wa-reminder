package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/blesta/wa-reminder/internal/config"
	"github.com/blesta/wa-reminder/internal/repository/postgres"
	"github.com/blesta/wa-reminder/internal/service"
)

type App struct {
	Config          *config.Config
	Logger          zerolog.Logger
	DB              *pgxpool.Pool
	Repo            *postgres.Repository
	RuntimeSettings *service.RuntimeSettingsService
	WAService       *service.WAService
	ReminderService *service.ReminderService
	AdminService    *service.AdminService
	WorkerService   *service.WorkerService
}

func New(ctx context.Context, cfg *config.Config, logger zerolog.Logger, db *pgxpool.Pool) (*App, error) {
	repo := postgres.NewRepository(db)
	runtimeSettings := service.NewRuntimeSettingsService(repo, cfg)
	waService := service.NewWAService(ctx, cfg, logger, repo)
	reminderService := service.NewReminderService(repo, runtimeSettings, cfg, logger)
	adminService := service.NewAdminService(repo, runtimeSettings, waService, logger)
	workerService := service.NewWorkerService(repo, waService, runtimeSettings, cfg, logger)

	return &App{
		Config:          cfg,
		Logger:          logger,
		DB:              db,
		Repo:            repo,
		RuntimeSettings: runtimeSettings,
		WAService:       waService,
		ReminderService: reminderService,
		AdminService:    adminService,
		WorkerService:   workerService,
	}, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := a.WAService.Start(ctx); err != nil {
		return err
	}

	if a.Config.WorkerEnabled {
		go a.WorkerService.Start(ctx, time.Duration(a.Config.WorkerPollIntervalMS)*time.Millisecond)
	}
	return nil
}
