package model

import "time"

type WAConnectionStatus string

const (
	WAStatusNeedQR       WAConnectionStatus = "need_qr"
	WAStatusConnecting   WAConnectionStatus = "connecting"
	WAStatusConnected    WAConnectionStatus = "connected"
	WAStatusDisconnected WAConnectionStatus = "disconnected"
	WAStatusError        WAConnectionStatus = "error"
)

type WASession struct {
	ConnectionStatus WAConnectionStatus `json:"status"`
	PhoneMasked      *string            `json:"phone_masked,omitempty"`
	WAJID            *string            `json:"wa_jid,omitempty"`
	LastConnectedAt  *time.Time         `json:"last_connected_at,omitempty"`
	LastSeenAt       *time.Time         `json:"last_seen_at,omitempty"`
}

type WAQRCode struct {
	QRCode           string `json:"qr_code"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type DeliveryListItem struct {
	ID           int64      `json:"id"`
	JobID        int64      `json:"job_id"`
	ExternalID   string     `json:"external_id"`
	AttemptNo    int        `json:"attempt_no"`
	Phone        string     `json:"phone"`
	ServiceName  *string    `json:"service_name,omitempty"`
	Status       string     `json:"status"`
	ErrorCode    *string    `json:"error_code,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
}
