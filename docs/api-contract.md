# API Contract v1

## External API (`/api/v1`)

Semua endpoint wajib header:
- `Authorization: Bearer <token>`
- `Content-Type: application/json`
- `Idempotency-Key` (opsional, disarankan)

### `POST /reminders`
Membuat 1 reminder job.

Request:
```json
{
  "external_id": "inv-2026-0001",
  "phone": "6281234567890",
  "customer_name": "Budi",
  "service_name": "Hosting Basic",
  "expired_at": "2026-04-30T10:00:00+07:00",
  "template_code": "reminder_h3",
  "template_vars": {
    "invoice_no": "INV-001"
  },
  "send_at": "2026-04-27T09:00:00+07:00",
  "metadata": {
    "tenant_id": "t_001"
  }
}
```

Response:
```json
{
  "success": true,
  "data": {
    "job_id": "f8cb3d8e-a8f7-4dd2-8901-c0d138a7de01",
    "status": "scheduled",
    "queued_at": "2026-04-23T10:00:00Z"
  },
  "meta": {
    "request_id": "req_01",
    "timestamp": "2026-04-23T10:00:00Z"
  }
}
```

### `POST /reminders/bulk`
Membuat banyak reminder job (maks 100 item).

### `GET /reminders/{job_id}`
Mengecek status job.

### `POST /reminders/{job_id}/cancel`
Membatalkan job jika status masih `pending/scheduled/retrying`.

## Admin API (`/admin-api/v1`)

Semua endpoint admin pakai Basic Auth.

### Session/WA
- `GET /wa/status`
- `GET /wa/qr`
- `POST /wa/reconnect`
- `POST /wa/logout`

### Stats/Delivery
- `GET /stats/overview?range=today|7d|30d`
- `GET /deliveries?page=1&limit=20&status=sent&search=6281&from=2026-04-01&to=2026-04-30`
- `GET /deliveries/{id}`
- `GET /deliveries/export.csv?status=&search=&from=&to=`

### Queue Control
- `POST /queue/pause`
- `POST /queue/resume`

## Health
- `GET /health/live`
- `GET /health/ready`

## Job Lifecycle
- `pending`
- `scheduled`
- `processing`
- `retrying`
- `sent`
- `failed_permanent`
- `cancelled`

## Rule Matrix Ringkas
- `INVALID_PHONE_FORMAT` -> `failed_permanent`, masuk `wa_unreachable_numbers`
- `NOT_ON_WHATSAPP` -> `failed_permanent`, masuk `wa_unreachable_numbers`
- `WA_JID_INVALID` -> `failed_permanent`, masuk `wa_unreachable_numbers`
- `WA_NOT_CONNECTED`/`WA_SEND_TIMEOUT`/`WA_SEND_ERROR` -> `retrying` (backoff)
- `MAX_ATTEMPTS_EXCEEDED` -> `failed_permanent`

## Catatan Operasional
- Precheck nomor via `IsOnWhatsApp` sebelum kirim.
- Typing status dikirim 5 detik sebelum pengiriman.
- Delay acak antar pesan mengikuti runtime setting (`delay_min_seconds`-`delay_max_seconds`).
- Runtime setting diambil dari tabel `app_settings` dengan fallback `.env`.
- Point frontend dashboard penuh dan hardening lanjutan dicatat terpisah (tidak dieksekusi di fase ini).

## Point 10 Dashboard (MVP)

- Route UI: `GET /admin` (Basic Auth)
- Static assets: `/admin/static/*`
- Frontend stack: Bootstrap 5 (CDN) + Vanilla JS modular
- Realtime:
  - WA status polling 3 detik
  - Stats polling 10-15 detik
- Delivery filter:
  - status
  - search (phone/service/external_id)
  - advanced date range (`from`, `to`)
- CSV export mengikuti filter aktif lewat endpoint `export.csv`
