-- 0008_device_voice_consent: the consent behind devices.voice_id (#107).
-- voice_consent_at is when the owner gave consent with the uploaded audio and
-- voice_consent_sha256 is the SHA-256 of that audio: the server keeps the
-- hash, never the audio. Both NULL while the device has no cloned voice.
ALTER TABLE devices
    ADD COLUMN voice_consent_at TIMESTAMPTZ,
    ADD COLUMN voice_consent_sha256 BYTEA;
