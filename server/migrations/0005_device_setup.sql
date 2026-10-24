-- 0005_device_setup: what "Mamá" needs per device (#104). user_name and
-- safe_phrase come with POST /devices; voice_id is the cloned voice, written
-- by the voice ticket. All NULL until set. See docs/contract.md.
ALTER TABLE devices
    ADD COLUMN user_name TEXT,
    ADD COLUMN safe_phrase TEXT,
    ADD COLUMN voice_id TEXT;
