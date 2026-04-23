package migration

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
)

func Run(ctx context.Context, databaseURL, migrationsDir string, logger zerolog.Logger) error {
	goose.SetBaseFS(nil)
	goose.SetTableName("goose_db_version")

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open sql db for migrations: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db for migrations: %w", err)
	}

	logger.Info().Str("dir", migrationsDir).Msg("running migrations")
	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	logger.Info().Msg("migrations finished")
	return nil
}
