-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE wa_connection_status AS ENUM (
  'need_qr', 'connecting', 'connected', 'disconnected', 'error'
);

CREATE TYPE reminder_status AS ENUM (
  'pending',
  'scheduled',
  'processing',
  'retrying',
  'sent',
  'failed_permanent',
  'cancelled'
);

CREATE TYPE attempt_status AS ENUM (
  'precheck_failed',
  'send_failed',
  'sent'
);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE api_clients (
  id BIGSERIAL PRIMARY KEY,
  client_name VARCHAR(100) NOT NULL,
  api_key_hash TEXT NOT NULL UNIQUE,
  is_active BOOLEAN NOT NULL DEFAULT true,
  rate_limit_per_minute INT NOT NULL DEFAULT 60,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER trg_api_clients_updated_at
BEFORE UPDATE ON api_clients
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE wa_session_singleton (
  id SMALLINT PRIMARY KEY CHECK (id = 1),
  connection_status wa_connection_status NOT NULL DEFAULT 'need_qr',
  phone_e164 VARCHAR(20),
  phone_masked VARCHAR(30),
  wa_jid VARCHAR(100),
  push_name VARCHAR(120),
  is_active BOOLEAN NOT NULL DEFAULT true,
  last_connected_at TIMESTAMPTZ,
  last_seen_at TIMESTAMPTZ,
  qr_last_generated_at TIMESTAMPTZ,
  qr_expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO wa_session_singleton (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TRIGGER trg_wa_session_singleton_updated_at
BEFORE UPDATE ON wa_session_singleton
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE app_settings (
  key VARCHAR(80) PRIMARY KEY,
  value JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE reminder_jobs (
  id BIGSERIAL PRIMARY KEY,
  job_uuid UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
  client_id BIGINT NOT NULL REFERENCES api_clients(id),
  external_id VARCHAR(120) NOT NULL,
  idempotency_key VARCHAR(120),
  phone_e164 VARCHAR(20) NOT NULL,
  canonical_jid VARCHAR(100),
  customer_name VARCHAR(120),
  service_name VARCHAR(120),
  expired_at TIMESTAMPTZ,
  template_code VARCHAR(80) NOT NULL,
  template_vars JSONB NOT NULL DEFAULT '{}'::jsonb,
  rendered_message TEXT,
  status reminder_status NOT NULL DEFAULT 'pending',
  is_whatsapp_registered BOOLEAN,
  priority SMALLINT NOT NULL DEFAULT 100,
  send_at TIMESTAMPTZ NOT NULL,
  next_attempt_at TIMESTAMPTZ,
  attempt_count INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 3,
  last_error_code VARCHAR(60),
  last_error_message TEXT,
  locked_by VARCHAR(80),
  locked_at TIMESTAMPTZ,
  sent_at TIMESTAMPTZ,
  cancelled_at TIMESTAMPTZ,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uq_client_external UNIQUE (client_id, external_id)
);

CREATE INDEX idx_reminder_jobs_queue_pick
  ON reminder_jobs (status, send_at, next_attempt_at, priority, id);

CREATE INDEX idx_reminder_jobs_phone
  ON reminder_jobs (phone_e164);

CREATE INDEX idx_reminder_jobs_created
  ON reminder_jobs (created_at DESC);

CREATE INDEX idx_reminder_jobs_idempotency
  ON reminder_jobs (idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE TRIGGER trg_reminder_jobs_updated_at
BEFORE UPDATE ON reminder_jobs
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE delivery_attempts (
  id BIGSERIAL PRIMARY KEY,
  job_id BIGINT NOT NULL REFERENCES reminder_jobs(id) ON DELETE CASCADE,
  attempt_no INT NOT NULL,
  status attempt_status NOT NULL,
  precheck_is_on_whatsapp BOOLEAN,
  precheck_response JSONB,
  typing_started_at TIMESTAMPTZ,
  typing_ended_at TIMESTAMPTZ,
  typing_duration_ms INT,
  wa_message_id VARCHAR(120),
  provider_response JSONB,
  error_code VARCHAR(60),
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uq_job_attempt UNIQUE (job_id, attempt_no)
);

CREATE INDEX idx_delivery_attempts_job_id
  ON delivery_attempts (job_id, attempt_no DESC);

CREATE INDEX idx_delivery_attempts_created
  ON delivery_attempts (created_at DESC);

CREATE TABLE wa_number_check_cache (
  phone_e164 VARCHAR(20) PRIMARY KEY,
  canonical_jid VARCHAR(100),
  is_on_whatsapp BOOLEAN NOT NULL,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_wa_number_check_cache_expires
  ON wa_number_check_cache (expires_at);

CREATE TABLE wa_unreachable_numbers (
  phone_e164 VARCHAR(20) PRIMARY KEY,
  canonical_jid VARCHAR(100),
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  failure_count INT NOT NULL DEFAULT 1,
  last_reason_code VARCHAR(60) NOT NULL,
  last_reason_message TEXT,
  is_blocked_for_sending BOOLEAN NOT NULL DEFAULT true,
  next_recheck_at TIMESTAMPTZ,
  notes TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wa_unreachable_blocked
  ON wa_unreachable_numbers (is_blocked_for_sending, next_recheck_at);

CREATE TRIGGER trg_wa_unreachable_numbers_updated_at
BEFORE UPDATE ON wa_unreachable_numbers
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE wa_unreachable_events (
  id BIGSERIAL PRIMARY KEY,
  phone_e164 VARCHAR(20) NOT NULL,
  job_id BIGINT REFERENCES reminder_jobs(id) ON DELETE SET NULL,
  reason_code VARCHAR(60) NOT NULL,
  reason_message TEXT,
  raw_payload JSONB,
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wa_unreachable_events_phone
  ON wa_unreachable_events (phone_e164, detected_at DESC);

CREATE INDEX idx_wa_unreachable_events_job
  ON wa_unreachable_events (job_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS wa_unreachable_events;
DROP TABLE IF EXISTS wa_unreachable_numbers;
DROP TABLE IF EXISTS wa_number_check_cache;
DROP TABLE IF EXISTS delivery_attempts;
DROP TABLE IF EXISTS reminder_jobs;
DROP TABLE IF EXISTS app_settings;
DROP TABLE IF EXISTS wa_session_singleton;
DROP TABLE IF EXISTS api_clients;

DROP FUNCTION IF EXISTS set_updated_at();

DROP TYPE IF EXISTS attempt_status;
DROP TYPE IF EXISTS reminder_status;
DROP TYPE IF EXISTS wa_connection_status;
-- +goose StatementEnd
