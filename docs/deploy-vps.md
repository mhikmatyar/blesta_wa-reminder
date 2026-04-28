# Deploy ke VPS dengan GitHub Actions

Dokumen ini menjelaskan setup sekali saja di VPS, lalu deployment otomatis saat `push` ke branch `main`.

## Ringkasan Arsitektur

- Workflow GitHub Actions build binary `blesta-wa-reminder` (Linux `amd64`).
- Workflow mengirim binary ke `/opt/blesta-wa-reminder/blesta-wa-reminder.new`.
- Binary diaktifkan lewat rename ke `/opt/blesta-wa-reminder/blesta-wa-reminder`.
- Service `wa-reminder` di-restart oleh `systemd`.
- Asset admin sudah di-embed ke binary, sehingga deploy hanya butuh binary + `.env`.

## 1) Setup awal di VPS

Jalankan di VPS:

```bash
sudo mkdir -p /opt/blesta-wa-reminder
sudo touch /opt/blesta-wa-reminder/.env
sudo chmod 600 /opt/blesta-wa-reminder/.env
```

Isi `/opt/blesta-wa-reminder/.env` berdasarkan `.env.example`, minimal:

- `DATABASE_URL`
- `API_BEARER_TOKEN`
- `ADMIN_BASIC_USER`
- `ADMIN_BASIC_PASS`
- `APP_ENV=production`
- `APP_PORT=8080`

## 2) Setup systemd service

Buat file `/etc/systemd/system/wa-reminder.service`:

```ini
[Unit]
Description=Blesta WA Reminder Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/blesta-wa-reminder
EnvironmentFile=/opt/blesta-wa-reminder/.env
ExecStart=/opt/blesta-wa-reminder/blesta-wa-reminder
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Aktifkan service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now wa-reminder
sudo systemctl status wa-reminder
```

Lihat log:

```bash
sudo journalctl -u wa-reminder -f
```

## 3) Setup SSH deploy key khusus

### Di mesin admin/lokal

Generate key:

```bash
ssh-keygen -t ed25519 -C "gha-deploy-wa-reminder" -f ~/.ssh/gha_wa_reminder
```

Output:

- Private key: `~/.ssh/gha_wa_reminder`
- Public key: `~/.ssh/gha_wa_reminder.pub`

### Pasang public key ke VPS (user `ubuntu`)

```bash
ssh-copy-id -i ~/.ssh/gha_wa_reminder.pub -p <PORT_SSH> ubuntu@<HOST_VPS>
```

Atau manual append ke `/home/ubuntu/.ssh/authorized_keys`.

### Konfigurasi sudoers untuk restart service (non-interactive)

Cari path `systemctl`:

```bash
which systemctl
```

Buat file sudoers:

```bash
sudo visudo -f /etc/sudoers.d/wa-reminder-deploy
```

Isi (sesuaikan path `systemctl` sesuai output `which`):

```text
ubuntu ALL=(ALL) NOPASSWD:/usr/bin/systemctl restart wa-reminder,/usr/bin/systemctl status wa-reminder,/usr/bin/systemctl is-active wa-reminder
```

Validasi:

```bash
sudo visudo -cf /etc/sudoers.d/wa-reminder-deploy
```

## 4) GitHub Repository Secrets

Masuk ke GitHub repository: `Settings -> Secrets and variables -> Actions -> New repository secret`.

Tambah secrets berikut:

- `VPS_HOST`: domain atau IP VPS
- `VPS_PORT`: port SSH VPS (contoh `22`)
- `VPS_USER`: `ubuntu`
- `VPS_SSH_KEY`: isi private key dari `~/.ssh/gha_wa_reminder`

Cara mengambil private key (copy seluruh isi termasuk header/footer):

```bash
cat ~/.ssh/gha_wa_reminder
```

## 5) Cara mengetahui host/port/user dengan cepat

- Host VPS: IP/domain server yang biasa dipakai SSH.
- Port SSH:

```bash
sudo ss -tulpen | grep ssh
```

atau cek:

```bash
sudo rg "^Port" /etc/ssh/sshd_config
```

- User deploy: gunakan `ubuntu` (sesuai setup SSH key di atas).

## 6) Checklist verifikasi setelah deploy

- GitHub Actions workflow `CI CD VPS Deploy` status sukses di commit `main`.
- Service aktif:

```bash
sudo systemctl status wa-reminder
```

- Health endpoint merespons:

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
```

- Halaman admin terbuka via reverse proxy dan asset CSS/JS termuat normal.
- Port `8080` tidak terbuka publik:
  - Batasi firewall hanya internal/reverse proxy.
  - Jangan expose admin API langsung ke internet.
