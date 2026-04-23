package model

import "time"

type ReminderStatus string

const (
	ReminderStatusPending         ReminderStatus = "pending"
	ReminderStatusScheduled       ReminderStatus = "scheduled"
	ReminderStatusProcessing      ReminderStatus = "processing"
	ReminderStatusRetrying        ReminderStatus = "retrying"
	ReminderStatusSent            ReminderStatus = "sent"
	ReminderStatusFailedPermanent ReminderStatus = "failed_permanent"
	ReminderStatusCancelled       ReminderStatus = "cancelled"
)

type ReminderJob struct {
	ID                   int64                  `json:"id"`
	JobUUID              string                 `json:"job_uuid"`
	ClientID             int64                  `json:"client_id"`
	ExternalID           string                 `json:"external_id"`
	IdempotencyKey       *string                `json:"idempotency_key,omitempty"`
	PhoneE164            string                 `json:"phone"`
	CanonicalJID         *string                `json:"canonical_jid,omitempty"`
	CustomerName         *string                `json:"customer_name,omitempty"`
	ServiceName          *string                `json:"service_name,omitempty"`
	ExpiredAt            *time.Time             `json:"expired_at,omitempty"`
	TemplateCode         string                 `json:"template_code"`
	TemplateVars         map[string]interface{} `json:"template_vars"`
	RenderedMessage      *string                `json:"rendered_message,omitempty"`
	Status               ReminderStatus         `json:"status"`
	IsWhatsAppRegistered *bool                  `json:"is_whatsapp_registered,omitempty"`
	Priority             int16                  `json:"priority"`
	SendAt               time.Time              `json:"send_at"`
	NextAttemptAt        *time.Time             `json:"next_attempt_at,omitempty"`
	AttemptCount         int                    `json:"attempt_count"`
	MaxAttempts          int                    `json:"max_attempts"`
	LastErrorCode        *string                `json:"last_error_code,omitempty"`
	LastErrorMessage     *string                `json:"last_error_message,omitempty"`
	SentAt               *time.Time             `json:"sent_at,omitempty"`
	CancelledAt          *time.Time             `json:"cancelled_at,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

type CreateReminderRequest struct {
	ExternalID   string                 `json:"external_id" validate:"required,max=120"`
	Phone        string                 `json:"phone" validate:"required,max=20"`
	CustomerName string                 `json:"customer_name" validate:"max=120"`
	ServiceName  string                 `json:"service_name" validate:"max=120"`
	ExpiredAt    *time.Time             `json:"expired_at"`
	TemplateCode string                 `json:"template_code" validate:"required,max=80"`
	TemplateVars map[string]interface{} `json:"template_vars"`
	SendAt       *time.Time             `json:"send_at"`
	Metadata     map[string]interface{} `json:"metadata"`
}

type CreateBulkReminderRequest struct {
	Items []CreateReminderRequest `json:"items" validate:"required,min=1,max=100,dive"`
}

type QueueRuntimeSettings struct {
	TypingDurationMS int
	DelayMinSeconds  int
	DelayMaxSeconds  int
	RetryBackoffSec  []int
	MaxAttempts      int
	DailyCap         int
	HourlyCap        int
	SendWindowStart  string
	SendWindowEnd    string
	QueuePaused      bool
}
