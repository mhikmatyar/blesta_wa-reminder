package postgres

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/blesta/wa-reminder/internal/domain/model"
)

var ErrNotFound = errors.New("not found")

var allowedReminderTemplateCodes = map[string]struct{}{
	"expiry_h14": {},
	"expiry_h7":  {},
	"expiry_h3":  {},
}

type Repository struct {
	db *pgxpool.Pool
}

type NumberCacheResult struct {
	PhoneE164    string
	CanonicalJID *string
	IsOnWhatsApp bool
	CheckedAt    time.Time
	ExpiresAt    time.Time
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateReminder(ctx context.Context, clientID int64, req model.CreateReminderRequest, idempotencyKey string, maxAttempts int) (*model.ReminderJob, error) {
	templateVars := req.TemplateVars
	if templateVars == nil {
		templateVars = map[string]interface{}{}
	}
	metadata := req.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	sendAt := time.Now().UTC()
	status := model.ReminderStatusPending
	if req.SendAt != nil {
		sendAt = req.SendAt.UTC()
		status = model.ReminderStatusScheduled
	}

	templateVarsJSON, _ := json.Marshal(templateVars)
	metadataJSON, _ := json.Marshal(metadata)

	row := r.db.QueryRow(ctx, `
		INSERT INTO reminder_jobs (
			client_id, external_id, idempotency_key, phone_e164, customer_name, service_name, expired_at,
			template_code, template_vars, status, send_at, max_attempts, metadata
		) VALUES (
			$1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13
		)
		ON CONFLICT (client_id, external_id) DO UPDATE SET updated_at = now()
		RETURNING id, job_uuid, client_id, external_id, phone_e164, template_code, status, send_at, attempt_count, max_attempts, created_at, updated_at
	`, clientID, req.ExternalID, nullableString(idempotencyKey), req.Phone, req.CustomerName, req.ServiceName, req.ExpiredAt, req.TemplateCode, templateVarsJSON, status, sendAt, maxAttempts, metadataJSON)

	var job model.ReminderJob
	if err := row.Scan(
		&job.ID, &job.JobUUID, &job.ClientID, &job.ExternalID, &job.PhoneE164, &job.TemplateCode, &job.Status,
		&job.SendAt, &job.AttemptCount, &job.MaxAttempts, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		return nil, err
	}
	job.TemplateVars = templateVars
	return &job, nil
}

func (r *Repository) GetReminderByJobUUID(ctx context.Context, jobUUID string) (*model.ReminderJob, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, job_uuid, client_id, external_id, phone_e164, template_code, status, send_at, next_attempt_at,
		       attempt_count, max_attempts, last_error_code, last_error_message, sent_at, cancelled_at, created_at, updated_at,
		       is_whatsapp_registered, canonical_jid
		FROM reminder_jobs WHERE job_uuid = $1
	`, jobUUID)

	var job model.ReminderJob
	var lastErrCode, lastErrMessage, canonicalJID *string
	var isWA *bool
	var nextAttemptAt, sentAt, cancelledAt *time.Time

	if err := row.Scan(
		&job.ID, &job.JobUUID, &job.ClientID, &job.ExternalID, &job.PhoneE164, &job.TemplateCode, &job.Status, &job.SendAt, &nextAttemptAt,
		&job.AttemptCount, &job.MaxAttempts, &lastErrCode, &lastErrMessage, &sentAt, &cancelledAt, &job.CreatedAt, &job.UpdatedAt, &isWA, &canonicalJID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	job.NextAttemptAt = nextAttemptAt
	job.LastErrorCode = lastErrCode
	job.LastErrorMessage = lastErrMessage
	job.SentAt = sentAt
	job.CancelledAt = cancelledAt
	job.IsWhatsAppRegistered = isWA
	job.CanonicalJID = canonicalJID
	return &job, nil
}

func (r *Repository) CancelReminder(ctx context.Context, jobUUID string) error {
	ct, err := r.db.Exec(ctx, `
		UPDATE reminder_jobs
		SET status='cancelled', cancelled_at=now(), updated_at=now()
		WHERE job_uuid=$1 AND status IN ('pending','scheduled','retrying')
	`, jobUUID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) PickDueJobs(ctx context.Context, batch int, workerID string) ([]model.ReminderJob, error) {
	rows, err := r.db.Query(ctx, `
		WITH picked AS (
			SELECT id
			FROM reminder_jobs
			WHERE status IN ('pending', 'scheduled', 'retrying')
			  AND send_at <= now()
			  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
			ORDER BY priority ASC, send_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE reminder_jobs j
		SET status='processing', locked_by=$2, locked_at=now(), updated_at=now()
		FROM picked
		WHERE j.id=picked.id
		RETURNING j.id, j.job_uuid, j.client_id, j.external_id, j.phone_e164, j.template_code, j.template_vars, j.status, j.send_at, j.attempt_count, j.max_attempts, j.customer_name, j.service_name, j.expired_at
	`, batch, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.ReminderJob, 0, batch)
	for rows.Next() {
		var job model.ReminderJob
		var templateVars []byte
		var customerName, serviceName *string
		var expiredAt *time.Time
		if err := rows.Scan(
			&job.ID, &job.JobUUID, &job.ClientID, &job.ExternalID, &job.PhoneE164, &job.TemplateCode, &templateVars,
			&job.Status, &job.SendAt, &job.AttemptCount, &job.MaxAttempts, &customerName, &serviceName, &expiredAt,
		); err != nil {
			return nil, err
		}
		job.CustomerName = customerName
		job.ServiceName = serviceName
		job.ExpiredAt = expiredAt
		job.TemplateVars = map[string]interface{}{}
		_ = json.Unmarshal(templateVars, &job.TemplateVars)
		out = append(out, job)
	}
	return out, rows.Err()
}

func (r *Repository) MarkJobSent(ctx context.Context, jobID int64, waMessageID string, typingMS int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE reminder_jobs
		SET status='sent', sent_at=now(), updated_at=now(), last_error_code=NULL, last_error_message=NULL
		WHERE id=$1
	`, jobID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO delivery_attempts(job_id, attempt_no, status, typing_started_at, typing_ended_at, typing_duration_ms, wa_message_id, created_at)
		SELECT id, attempt_count + 1, 'sent', now() - ($2 * interval '1 millisecond'), now(), $2, $3, now()
		FROM reminder_jobs WHERE id=$1
	`, jobID, typingMS, waMessageID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) MarkJobFailed(ctx context.Context, job model.ReminderJob, errCode, errMessage string, retryInSec int, permanent bool) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	attemptCount := job.AttemptCount + 1
	status := model.ReminderStatusRetrying
	var nextAttemptAt any = nil
	if permanent || attemptCount >= job.MaxAttempts {
		status = model.ReminderStatusFailedPermanent
	} else {
		nextAttemptAt = time.Now().UTC().Add(time.Duration(retryInSec) * time.Second)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE reminder_jobs
		SET attempt_count=$2,
		    status=$3,
		    next_attempt_at=$4,
		    last_error_code=$5,
		    last_error_message=$6,
		    updated_at=now()
		WHERE id=$1
	`, job.ID, attemptCount, status, nextAttemptAt, errCode, errMessage); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO delivery_attempts(job_id, attempt_no, status, error_code, error_message, created_at)
		VALUES ($1,$2,$3,$4,$5,now())
	`, job.ID, attemptCount, map[bool]string{true: "precheck_failed", false: "send_failed"}[permanent], errCode, errMessage); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) SetWhatsAppCheckResult(ctx context.Context, jobID int64, isRegistered bool, canonicalJID *string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE reminder_jobs SET is_whatsapp_registered=$2, canonical_jid=$3, updated_at=now() WHERE id=$1
	`, jobID, isRegistered, canonicalJID)
	return err
}

func (r *Repository) GetNumberCache(ctx context.Context, phone string) (*NumberCacheResult, error) {
	row := r.db.QueryRow(ctx, `
		SELECT phone_e164, canonical_jid, is_on_whatsapp, checked_at, expires_at
		FROM wa_number_check_cache
		WHERE phone_e164=$1 AND expires_at > now()
	`, phone)
	var item NumberCacheResult
	if err := row.Scan(&item.PhoneE164, &item.CanonicalJID, &item.IsOnWhatsApp, &item.CheckedAt, &item.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (r *Repository) UpsertNumberCache(ctx context.Context, phone string, canonicalJID *string, isOnWhatsApp bool, ttlHours int) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wa_number_check_cache(phone_e164, canonical_jid, is_on_whatsapp, checked_at, expires_at)
		VALUES ($1,$2,$3,now(), now() + ($4 * interval '1 hour'))
		ON CONFLICT(phone_e164)
		DO UPDATE SET canonical_jid=EXCLUDED.canonical_jid, is_on_whatsapp=EXCLUDED.is_on_whatsapp, checked_at=EXCLUDED.checked_at, expires_at=EXCLUDED.expires_at
	`, phone, canonicalJID, isOnWhatsApp, ttlHours)
	return err
}

func (r *Repository) UpsertUnreachable(ctx context.Context, phone string, canonicalJID *string, reasonCode, reasonMessage string, recheckDays int, jobID *int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO wa_unreachable_numbers(phone_e164, canonical_jid, last_reason_code, last_reason_message, next_recheck_at)
		VALUES ($1,$2,$3,$4, now() + ($5 * interval '1 day'))
		ON CONFLICT(phone_e164) DO UPDATE
		SET canonical_jid=EXCLUDED.canonical_jid,
		    last_reason_code=EXCLUDED.last_reason_code,
		    last_reason_message=EXCLUDED.last_reason_message,
		    last_seen_at=now(),
		    failure_count=wa_unreachable_numbers.failure_count+1,
		    is_blocked_for_sending=true,
		    next_recheck_at=EXCLUDED.next_recheck_at,
		    updated_at=now()
	`, phone, canonicalJID, reasonCode, reasonMessage, recheckDays); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO wa_unreachable_events(phone_e164, job_id, reason_code, reason_message, detected_at)
		VALUES ($1,$2,$3,$4,now())
	`, phone, nullableInt64(jobID), reasonCode, reasonMessage); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) IsPhoneBlocked(ctx context.Context, phone string) (bool, error) {
	row := r.db.QueryRow(ctx, `
		SELECT is_blocked_for_sending, next_recheck_at
		FROM wa_unreachable_numbers
		WHERE phone_e164=$1
	`, phone)
	var blocked bool
	var nextRecheck pgtype.Timestamptz
	if err := row.Scan(&blocked, &nextRecheck); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if !blocked {
		return false, nil
	}
	if nextRecheck.Valid && nextRecheck.Time.Before(time.Now().UTC()) {
		return false, nil
	}
	return true, nil
}

func (r *Repository) GetWAStatus(ctx context.Context) (*model.WASession, error) {
	if err := r.ensureWASessionSingleton(ctx); err != nil {
		return nil, err
	}

	row := r.db.QueryRow(ctx, `
		SELECT connection_status, phone_masked, wa_jid, last_connected_at, last_seen_at
		FROM wa_session_singleton WHERE id=1
	`)
	var s model.WASession
	if err := row.Scan(&s.ConnectionStatus, &s.PhoneMasked, &s.WAJID, &s.LastConnectedAt, &s.LastSeenAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) UpdateWAStatus(ctx context.Context, status model.WAConnectionStatus, phoneMasked, waJID *string) error {
	if err := r.ensureWASessionSingleton(ctx); err != nil {
		return err
	}

	_, err := r.db.Exec(ctx, `
		UPDATE wa_session_singleton
		SET connection_status=$1,
		    phone_masked=COALESCE($2, phone_masked),
		    wa_jid=COALESCE($3, wa_jid),
		    last_seen_at=now(),
		    last_connected_at=CASE WHEN $1::wa_connection_status='connected'::wa_connection_status THEN now() ELSE last_connected_at END,
		    updated_at=now()
		WHERE id=1
	`, status, phoneMasked, waJID)
	return err
}

func (r *Repository) ClearWASession(ctx context.Context) error {
	if err := r.ensureWASessionSingleton(ctx); err != nil {
		return err
	}

	_, err := r.db.Exec(ctx, `
		UPDATE wa_session_singleton
		SET connection_status='need_qr', phone_e164=NULL, phone_masked=NULL, wa_jid=NULL, push_name=NULL, updated_at=now()
		WHERE id=1
	`)
	return err
}

func (r *Repository) ensureWASessionSingleton(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wa_session_singleton (id, connection_status, is_active)
		VALUES (1, 'need_qr', true)
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("ensure wa_session_singleton row: %w", err)
	}
	return nil
}

func (r *Repository) SetAppSetting(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO app_settings(key, value, updated_at)
		VALUES ($1,$2,now())
		ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()
	`, key, raw)
	return err
}

func (r *Repository) GetAppSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := r.db.Query(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]json.RawMessage)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, rows.Err()
}

func (r *Repository) ListReminderTemplates(ctx context.Context) ([]model.ReminderMessageTemplate, error) {
	rows, err := r.db.Query(ctx, `
		SELECT template_code, message_template, updated_at
		FROM reminder_message_templates
		ORDER BY template_code ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ReminderMessageTemplate, 0, 3)
	for rows.Next() {
		var item model.ReminderMessageTemplate
		if err := rows.Scan(&item.TemplateCode, &item.MessageTemplate, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetReminderTemplateByCode(ctx context.Context, code string) (*model.ReminderMessageTemplate, error) {
	row := r.db.QueryRow(ctx, `
		SELECT template_code, message_template, updated_at
		FROM reminder_message_templates
		WHERE template_code = $1
	`, strings.TrimSpace(code))

	var item model.ReminderMessageTemplate
	if err := row.Scan(&item.TemplateCode, &item.MessageTemplate, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (r *Repository) UpsertReminderTemplate(ctx context.Context, code, messageTemplate string) error {
	code = strings.TrimSpace(code)
	messageTemplate = strings.TrimSpace(messageTemplate)

	if _, ok := allowedReminderTemplateCodes[code]; !ok {
		return fmt.Errorf("invalid template code")
	}
	if messageTemplate == "" {
		return fmt.Errorf("message template is required")
	}

	_, err := r.db.Exec(ctx, `
		INSERT INTO reminder_message_templates(template_code, message_template, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT(template_code)
		DO UPDATE SET message_template = EXCLUDED.message_template, updated_at = now()
	`, code, messageTemplate)
	return err
}

func (r *Repository) GetStatsOverview(ctx context.Context, since time.Time) (map[string]int64, error) {
	row := r.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('pending','scheduled')) AS queued,
			COUNT(*) FILTER (WHERE status = 'processing') AS processing,
			COUNT(*) FILTER (WHERE status = 'sent' AND sent_at >= $1) AS sent,
			COUNT(*) FILTER (WHERE status = 'retrying') AS retrying,
			COUNT(*) FILTER (WHERE status = 'failed_permanent' AND updated_at >= $1) AS failed
		FROM reminder_jobs
	`, since)
	var queued, processing, sent, retrying, failed int64
	if err := row.Scan(&queued, &processing, &sent, &retrying, &failed); err != nil {
		return nil, err
	}
	return map[string]int64{
		"queued":     queued,
		"processing": processing,
		"sent":       sent,
		"retrying":   retrying,
		"failed":     failed,
	}, nil
}

func (r *Repository) ListDeliveries(ctx context.Context, status string, limit, offset int) ([]model.DeliveryListItem, error) {
	return r.ListDeliveriesFiltered(ctx, status, "", nil, nil, limit, offset)
}

func (r *Repository) ListDeliveriesFiltered(ctx context.Context, status, search string, from, to *time.Time, limit, offset int) ([]model.DeliveryListItem, error) {
	query := `
		SELECT da.id, da.job_id, rj.external_id, da.attempt_no, rj.phone_e164, rj.service_name, da.status, da.error_code, da.error_message, da.created_at, rj.sent_at
		FROM delivery_attempts da
		JOIN reminder_jobs rj ON rj.id = da.job_id
	`
	args := []any{}
	clauses := make([]string, 0, 4)
	argPos := 1
	if status != "" {
		clauses = append(clauses, fmt.Sprintf("da.status = $%d", argPos))
		args = append(args, status)
		argPos++
	}
	if search != "" {
		clauses = append(clauses, fmt.Sprintf("(rj.phone_e164 ILIKE $%d OR rj.service_name ILIKE $%d OR rj.external_id ILIKE $%d)", argPos, argPos, argPos))
		args = append(args, "%"+search+"%")
		argPos++
	}
	if from != nil {
		clauses = append(clauses, fmt.Sprintf("da.created_at >= $%d", argPos))
		args = append(args, from.UTC())
		argPos++
	}
	if to != nil {
		clauses = append(clauses, fmt.Sprintf("da.created_at <= $%d", argPos))
		args = append(args, to.UTC())
		argPos++
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	query += fmt.Sprintf(" ORDER BY da.created_at DESC LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.DeliveryListItem, 0, limit)
	for rows.Next() {
		var item model.DeliveryListItem
		if err := rows.Scan(&item.ID, &item.JobID, &item.ExternalID, &item.AttemptNo, &item.Phone, &item.ServiceName, &item.Status, &item.ErrorCode, &item.ErrorMessage, &item.CreatedAt, &item.SentAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) CountDeliveriesFiltered(ctx context.Context, status, search string, from, to *time.Time) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM delivery_attempts da
		JOIN reminder_jobs rj ON rj.id = da.job_id
	`
	args := []any{}
	clauses := make([]string, 0, 4)
	argPos := 1
	if status != "" {
		clauses = append(clauses, fmt.Sprintf("da.status = $%d", argPos))
		args = append(args, status)
		argPos++
	}
	if search != "" {
		clauses = append(clauses, fmt.Sprintf("(rj.phone_e164 ILIKE $%d OR rj.service_name ILIKE $%d OR rj.external_id ILIKE $%d)", argPos, argPos, argPos))
		args = append(args, "%"+search+"%")
		argPos++
	}
	if from != nil {
		clauses = append(clauses, fmt.Sprintf("da.created_at >= $%d", argPos))
		args = append(args, from.UTC())
		argPos++
	}
	if to != nil {
		clauses = append(clauses, fmt.Sprintf("da.created_at <= $%d", argPos))
		args = append(args, to.UTC())
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	row := r.db.QueryRow(ctx, query, args...)
	var total int64
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *Repository) GetDeliveryByID(ctx context.Context, id int64) (*model.DeliveryListItem, error) {
	row := r.db.QueryRow(ctx, `
		SELECT da.id, da.job_id, rj.external_id, da.attempt_no, rj.phone_e164, rj.service_name, da.status, da.error_code, da.error_message, da.created_at, rj.sent_at
		FROM delivery_attempts da
		JOIN reminder_jobs rj ON rj.id = da.job_id
		WHERE da.id = $1
	`, id)
	var item model.DeliveryListItem
	if err := row.Scan(&item.ID, &item.JobID, &item.ExternalID, &item.AttemptNo, &item.Phone, &item.ServiceName, &item.Status, &item.ErrorCode, &item.ErrorMessage, &item.CreatedAt, &item.SentAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (r *Repository) ExportDeliveriesCSV(ctx context.Context, status, search string, from, to *time.Time) ([]byte, error) {
	items, err := r.ListDeliveriesFiltered(ctx, status, search, from, to, 5000, 0)
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)
	_ = w.Write([]string{"id", "job_id", "external_id", "phone", "service_name", "status", "attempt_no", "error_code", "error_message", "created_at", "sent_at"})
	for _, it := range items {
		service := ""
		if it.ServiceName != nil {
			service = *it.ServiceName
		}
		errCode := ""
		if it.ErrorCode != nil {
			errCode = *it.ErrorCode
		}
		errMsg := ""
		if it.ErrorMessage != nil {
			errMsg = *it.ErrorMessage
		}
		sentAt := ""
		if it.SentAt != nil {
			sentAt = it.SentAt.Format(time.RFC3339)
		}
		_ = w.Write([]string{
			fmt.Sprintf("%d", it.ID),
			fmt.Sprintf("%d", it.JobID),
			it.ExternalID,
			it.Phone,
			service,
			it.Status,
			fmt.Sprintf("%d", it.AttemptNo),
			errCode,
			errMsg,
			it.CreatedAt.Format(time.RFC3339),
			sentAt,
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func (r *Repository) CountSentSince(ctx context.Context, since time.Time) (int64, error) {
	row := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM reminder_jobs WHERE status='sent' AND sent_at >= $1`, since)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.Ping(ctx)
}

func (r *Repository) DB() *pgxpool.Pool {
	return r.db
}

func (r *Repository) EnsureDefaultClient(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO api_clients(id, client_name, api_key_hash, is_active)
		VALUES (1, 'default-client', 'managed-by-env-token', true)
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("ensure default client: %w", err)
	}
	return nil
}
