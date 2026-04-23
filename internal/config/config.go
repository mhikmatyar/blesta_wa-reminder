package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppName     string
	AppEnv      string
	AppPort     string
	AppTimeZone string

	DatabaseURL          string
	DBMaxConns           int32
	DBMinConns           int32
	DBMaxConnLifetimeMin int

	LogLevel string

	APIBearerToken string
	AdminBasicUser string
	AdminBasicPass string

	DefaultTypingDurationMS int
	DefaultDelayMinSec      int
	DefaultDelayMaxSec      int
	DefaultRetryBackoffSec  []int
	DefaultMaxAttempts      int
	DefaultDailyCap         int
	DefaultHourlyCap        int
	DefaultSendWindowStart  string
	DefaultSendWindowEnd    string

	NumberCheckCacheTTLHours int
	UnreachableRecheckDays   int

	WorkerEnabled        bool
	WorkerPollIntervalMS int
	WorkerBatchSize      int
	WorkerID             string

	AutoMigrate bool
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		AppName:     getEnv("APP_NAME", "blesta-wa-reminder"),
		AppEnv:      getEnv("APP_ENV", "development"),
		AppPort:     getEnv("APP_PORT", "8080"),
		AppTimeZone: getEnv("APP_TIMEZONE", "Asia/Jakarta"),

		DatabaseURL:          getEnv("DATABASE_URL", ""),
		DBMaxConns:           int32(getEnvInt("DB_MAX_CONNS", 10)),
		DBMinConns:           int32(getEnvInt("DB_MIN_CONNS", 2)),
		DBMaxConnLifetimeMin: getEnvInt("DB_MAX_CONN_LIFETIME_MIN", 30),

		LogLevel: getEnv("LOG_LEVEL", "info"),

		APIBearerToken: getEnv("API_BEARER_TOKEN", ""),
		AdminBasicUser: getEnv("ADMIN_BASIC_USER", "admin"),
		AdminBasicPass: getEnv("ADMIN_BASIC_PASS", "change_me"),

		DefaultTypingDurationMS: getEnvInt("DEFAULT_TYPING_DURATION_MS", 5000),
		DefaultDelayMinSec:      getEnvInt("DEFAULT_DELAY_MIN_SEC", 30),
		DefaultDelayMaxSec:      getEnvInt("DEFAULT_DELAY_MAX_SEC", 120),
		DefaultRetryBackoffSec:  getEnvIntSlice("DEFAULT_RETRY_BACKOFF_SEC", []int{300, 900, 3600}),
		DefaultMaxAttempts:      getEnvInt("DEFAULT_MAX_ATTEMPTS", 3),
		DefaultDailyCap:         getEnvInt("DEFAULT_DAILY_CAP", 40),
		DefaultHourlyCap:        getEnvInt("DEFAULT_HOURLY_CAP", 20),
		DefaultSendWindowStart:  getEnv("DEFAULT_SEND_WINDOW_START", "08:00"),
		DefaultSendWindowEnd:    getEnv("DEFAULT_SEND_WINDOW_END", "20:30"),

		NumberCheckCacheTTLHours: getEnvInt("NUMBER_CHECK_CACHE_TTL_HOURS", 24),
		UnreachableRecheckDays:   getEnvInt("UNREACHABLE_RECHECK_DAYS", 7),

		WorkerEnabled:        getEnvBool("WORKER_ENABLED", true),
		WorkerPollIntervalMS: getEnvInt("WORKER_POLL_INTERVAL_MS", 2000),
		WorkerBatchSize:      getEnvInt("WORKER_BATCH_SIZE", 10),
		WorkerID:             getEnv("WORKER_ID", "worker-1"),

		AutoMigrate: getEnvBool("AUTO_MIGRATE", true),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.APIBearerToken == "" {
		return nil, fmt.Errorf("API_BEARER_TOKEN is required")
	}

	if _, err := time.LoadLocation(cfg.AppTimeZone); err != nil {
		return nil, fmt.Errorf("invalid APP_TIMEZONE: %w", err)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvIntSlice(key string, fallback []int) []int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	parts := strings.Split(val, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return fallback
		}
		out = append(out, n)
	}
	return out
}
