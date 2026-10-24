-- 0002_device_keys: device Ed25519 public key and its 8-byte key_id (#55). See docs/contract.md.
ALTER TABLE devices
    ADD COLUMN ed25519_public_key BYTEA UNIQUE CHECK (octet_length(ed25519_public_key) = 32),
    ADD COLUMN key_id BYTEA UNIQUE CHECK (octet_length(key_id) = 8),
    ADD CONSTRAINT devices_key_pair_chk CHECK ((ed25519_public_key IS NULL) = (key_id IS NULL));
