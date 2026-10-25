-- 0012_incident_cancelled: the person called off an online incident ("Fue sin querer",
-- POST /incidents/{id}/cancel). cancelled_at is when; NULL means not cancelled. A cancelled
-- incident places no further call and sends no family message that was not already sent.
-- The incident report (report/central_store.go) already reads this column when it exists.
ALTER TABLE incidents ADD COLUMN cancelled_at TIMESTAMPTZ;
