-- 0008_offline_incidents: an offline emergency (#57) that reaches the server over HTTP, SMS or
-- the BLE relay (#60) opens an incident, so the family is told and the report shows it.
-- Its trigger is 'offline': the 98-byte payload does not say which gesture started it.
-- It was 0007 until #107's 0007_device_voice_consent landed; it drops and re-adds the same
-- check, so a database that already ran it as 0007 applies it again with no change.
ALTER TABLE incidents DROP CONSTRAINT incidents_trigger_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_trigger_check
    CHECK (trigger IN ('volume_pattern', 'widget', 'tile', 'in_app', 'offline'));
