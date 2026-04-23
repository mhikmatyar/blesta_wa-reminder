package main

import (
	"context"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	application "github.com/blesta/wa-reminder/internal/app"
	"github.com/blesta/wa-reminder/internal/config"
	apphttp "github.com/blesta/wa-reminder/internal/http"
	"github.com/blesta/wa-reminder/internal/migration"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		stdlog.Fatalf("load config: %v", err)
	}

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	if cfg.AutoMigrate {
		if err := migration.Run(ctx, cfg.DatabaseURL, "migrations", logger); err != nil {
			stdlog.Fatalf("run migration: %v", err)
		}
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		stdlog.Fatalf("parse db url: %v", err)
	}
	poolCfg.MaxConns = cfg.DBMaxConns
	poolCfg.MinConns = cfg.DBMinConns
	poolCfg.MaxConnLifetime = time.Duration(cfg.DBMaxConnLifetimeMin) * time.Minute

	db, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		stdlog.Fatalf("connect db: %v", err)
	}
	defer db.Close()

	app, err := application.New(ctx, cfg, logger, db)
	if err != nil {
		stdlog.Fatalf("init app: %v", err)
	}

	if err := app.Repo.EnsureDefaultClient(ctx); err != nil {
		stdlog.Fatalf("ensure default client: %v", err)
	}

	if err := app.Start(ctx); err != nil {
		stdlog.Fatalf("start app services: %v", err)
	}

	router := apphttp.NewRouter(app)
	server := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info().Str("addr", server.Addr).Msg("HTTP server started")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("http server failed")
		}
	}()

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("graceful shutdown failed")
	}
	fmt.Println("server stopped")
}
