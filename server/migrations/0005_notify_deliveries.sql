-- 0005_notify_deliveries: each family message the notifier sent, and what
-- Kapso later reported about it (#76), so the incident report shows that
-- every relative was notified. Written only when the incident report is on.
-- A new file, not part of 0004: databases that already applied 0004 get it.
CREATE TABLE notify_deliveries (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    incident_id  UUID NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    recipient    TEXT NOT NULL,             -- masked ("+58 424 ···33"); the full number stays in devices.family_numbers
    channel      TEXT NOT NULL CHECK (channel IN ('whatsapp', 'sms')),
    message_id   TEXT UNIQUE,               -- the provider's id (Kapso: the WhatsApp wamid); NULL when the send failed
    status       TEXT NOT NULL CHECK (status IN ('accepted', 'failed', 'sent', 'delivered', 'read')),
    error        TEXT,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT now(), -- server clock: the provider answered the send
    delivered_at TIMESTAMPTZ,                        -- server clock: the status webhook arrived
    read_at      TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notify_deliveries_incident_idx ON notify_deliveries (incident_id, id);
