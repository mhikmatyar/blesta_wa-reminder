package service

import (
	"testing"
	"time"

	"github.com/blesta/wa-reminder/internal/domain/model"
)

func TestIsValidPhone(t *testing.T) {
	tests := []struct {
		phone string
		valid bool
	}{
		{phone: "6281234567890", valid: true},
		{phone: "081234567890", valid: false},
		{phone: "62abc", valid: false},
		{phone: "628123", valid: false},
	}

	for _, tt := range tests {
		if got := isValidPhone(tt.phone); got != tt.valid {
			t.Fatalf("isValidPhone(%q) = %v, want %v", tt.phone, got, tt.valid)
		}
	}
}

func TestPickRetry(t *testing.T) {
	settings := model.QueueRuntimeSettings{
		RetryBackoffSec: []int{300, 900, 3600},
	}

	if got := pickRetry(settings, 0); got != 300 {
		t.Fatalf("pickRetry first = %d", got)
	}
	if got := pickRetry(settings, 1); got != 900 {
		t.Fatalf("pickRetry second = %d", got)
	}
	if got := pickRetry(settings, 5); got != 3600 {
		t.Fatalf("pickRetry overflow = %d", got)
	}
}

func TestShouldProcessForWAStatus(t *testing.T) {
	tests := []struct {
		name   string
		status model.WAConnectionStatus
		want   bool
	}{
		{name: "connected", status: model.WAStatusConnected, want: true},
		{name: "need_qr", status: model.WAStatusNeedQR, want: false},
		{name: "connecting", status: model.WAStatusConnecting, want: false},
		{name: "disconnected", status: model.WAStatusDisconnected, want: false},
		{name: "error", status: model.WAStatusError, want: false},
	}

	for _, tt := range tests {
		if got := shouldProcessForWAStatus(tt.status); got != tt.want {
			t.Fatalf("%s: shouldProcessForWAStatus(%q) = %v, want %v", tt.name, tt.status, got, tt.want)
		}
	}
}

func TestRenderMessageTemplate(t *testing.T) {
	expiredAt := time.Date(2026, 5, 10, 15, 0, 0, 0, time.UTC)
	customer := "Budi"
	serviceName := "Hosting Basic"
	job := model.ReminderJob{
		CustomerName: &customer,
		ServiceName:  &serviceName,
		ExpiredAt:    &expiredAt,
	}

	got := renderMessageTemplate(
		"Halo {{customer_name}}, layanan {{service_name}} expired {{expired_date}}.",
		job,
		"Asia/Jakarta",
	)
	want := "Halo Budi, layanan Hosting Basic expired 10/05/2026."
	if got != want {
		t.Fatalf("renderMessageTemplate() = %q, want %q", got, want)
	}
}

func TestRenderMessageTemplateWithFallbackValues(t *testing.T) {
	job := model.ReminderJob{}
	got := renderMessageTemplate(
		"Halo {{customer_name}} - {{service_name}} - {{expired_date}}",
		job,
		"Asia/Jakarta",
	)
	want := "Halo Pelanggan - layanan - -"
	if got != want {
		t.Fatalf("renderMessageTemplate fallback = %q, want %q", got, want)
	}
}

func TestRenderMessageTemplateInvalidTimezone(t *testing.T) {
	expiredAt := time.Date(2026, 6, 1, 1, 30, 0, 0, time.UTC)
	job := model.ReminderJob{ExpiredAt: &expiredAt}
	got := renderMessageTemplate("{{expired_date}}", job, "Invalid/Timezone")
	want := "01/06/2026"
	if got != want {
		t.Fatalf("renderMessageTemplate invalid timezone = %q, want %q", got, want)
	}
}
