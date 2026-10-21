-- +goose Up
-- A trusted device lets a browser sign in with a PIN. The device token and the
-- browser marker are stored hashed, never in clear.
CREATE TABLE trusted_device (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    pin_hash text,
    name text NOT NULL,
    browser_hash bytea,
    failed_pins integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    last_mfa_at timestamptz,
    removed_at timestamptz
);
CREATE INDEX trusted_device_user_id_idx ON trusted_device (user_id);

-- A password change or a new TOTP secret invalidates every trusted device of
-- the user. Keeping this in the database also covers the reset-2fa command.
-- +goose StatementBegin
CREATE FUNCTION revoke_devices_on_credential_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
       OR NEW.totp_secret_enc IS DISTINCT FROM OLD.totp_secret_enc THEN
        UPDATE trusted_device
           SET removed_at = now(), pin_hash = NULL
         WHERE user_id = NEW.id AND removed_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER app_user_revoke_devices
AFTER UPDATE OF password_hash, totp_secret_enc ON app_user
FOR EACH ROW EXECUTE FUNCTION revoke_devices_on_credential_change();

-- +goose Down
DROP TRIGGER app_user_revoke_devices ON app_user;
DROP FUNCTION revoke_devices_on_credential_change();
DROP TABLE trusted_device;
