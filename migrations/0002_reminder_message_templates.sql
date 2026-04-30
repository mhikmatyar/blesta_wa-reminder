-- +goose Up
-- +goose StatementBegin
CREATE TABLE reminder_message_templates (
  template_code VARCHAR(80) PRIMARY KEY,
  message_template TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER trg_reminder_message_templates_updated_at
BEFORE UPDATE ON reminder_message_templates
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

INSERT INTO reminder_message_templates (template_code, message_template)
VALUES
  (
    'expiry_h30',
    'Halo {{customer_name}}, ini pengingat H-30 untuk layanan {{service_name}}. Masa aktif akan berakhir pada {{expired_date}}. Terima kasih.'
  ),
  (
    'expiry_h15',
    'Halo {{customer_name}}, ini pengingat H-15 untuk layanan {{service_name}}. Masa aktif akan berakhir pada {{expired_date}}. Terima kasih.'
  ),
  (
    'expiry_h7',
    'Halo {{customer_name}}, ini pengingat H-7 untuk layanan {{service_name}}. Masa aktif akan berakhir pada {{expired_date}}. Terima kasih.'
  )
ON CONFLICT (template_code) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS reminder_message_templates;
-- +goose StatementEnd
