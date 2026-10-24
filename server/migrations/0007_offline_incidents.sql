-- 0007_offline_incidents: an offline emergency (#57) that reaches the server over HTTP, SMS or
-- the BLE relay (#60) opens an incident, so the family is told and the report shows it.
-- Its trigger is 'offline': the 98-byte payload does not say which gesture started it.
ALTER TABLE incidents DROP CONSTRAINT incidents_trigger_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_trigger_check
    CHECK (trigger IN ('volume_pattern', 'widget', 'tile', 'in_app', 'offline'));
