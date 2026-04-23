# blesta-wa-reminder

Service API WhatsApp reminder berbasis Go untuk menerima data reminder dari service eksternal, memproses antrian, melakukan precheck nomor WhatsApp, dan mengirim pesan via `whatsmeow`.

## Fitur Saat Ini

- External API:
  - `POST /api/v1/reminders`
  - `POST /api/v1/reminders/bulk`
  - `GET /api/v1/reminders/:job_id`
  - `POST /api/v1/reminders/:job_id/cancel`
- Admin API:
  - `GET /admin-api/v1/wa/status`
  - `GET /admin-api/v1/wa/qr`
  - `POST /admin-api/v1/wa/reconnect`
  - `POST /admin-api/v1/wa/logout`
  - `GET /admin-api/v1/stats/overview`
  - `GET /admin-api/v1/deliveries`
  - `GET /admin-api/v1/deliveries/:id`
  - `POST /admin-api/v1/queue/pause`
  - `POST /admin-api/v1/queue/resume`
- Queue worker berbasis PostgreSQL (`FOR UPDATE SKIP LOCKED`)
- Rule matrix pengiriman:
  - validasi format nomor
  - cek nomor via `IsOnWhatsApp`
  - typing indicator 5 detik sebelum kirim
  - retry backoff untuk transient error
  - tracking unreachable numbers
- Runtime settings hybrid:
  - baseline dari `.env`
  - override dari tabel `app_settings`
- Auto migration saat startup menggunakan Goose

## Tech Stack

- Go + Gin
- PostgreSQL
- `go.mau.fi/whatsmeow`
- Goose migration
- Air (live reload development)

## Prasyarat

- Go `1.24+`
- PostgreSQL aktif
- Nomor WhatsApp siap dipairing via QR

## Menjalankan Secara Lokal

1. Salin env:
   - `cp .env.example .env`
2. Sesuaikan nilai penting di `.env`:
   - `DATABASE_URL`
   - `API_BEARER_TOKEN`
   - `ADMIN_BASIC_USER` / `ADMIN_BASIC_PASS`
3. Jalankan service:
   - mode normal: `make run`
   - mode development (air): `make dev`

Server default akan jalan di `:8080`.

## Database & Migrasi

- File migrasi utama: `migrations/0001_init.sql`
- Auto migrate aktif via `AUTO_MIGRATE=true` (default)
- Goose hanya menjalankan migrasi yang belum pernah dieksekusi

## Dokumen Referensi

- Kontrak API: `docs/api-contract.md`
- Skema DDL: `docs/ddl.sql`
- Backlog point 10-11: `docs/plan-10-11-backlog.md`

## Testing

Jalankan:

```bash
make test
```

## Catatan Keamanan Minimum

- Endpoint `/api/v1/*` wajib Bearer token
- Endpoint `/admin-api/v1/*` wajib Basic Auth
- Jangan expose admin API ke publik tanpa proteksi tambahan (reverse proxy + firewall/IP allowlist)
