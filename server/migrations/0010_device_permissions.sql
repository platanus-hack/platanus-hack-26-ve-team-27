-- 0010_device_permissions: the phone's permissions (#125, approved by the Tech lead on Oct 24).
-- incidents.device_state is the snapshot an AlertEvent carries at the gesture (JSON, as the
-- phone sent it); NULL from an app that sends none. device_permission_events is when each
-- permission was granted or lost, as the phone saw it (POST /devices/{device_id}/permissions);
-- the key makes a resent event a no-op (ON CONFLICT DO NOTHING).
ALTER TABLE incidents ADD COLUMN device_state JSONB;

CREATE TABLE device_permission_events (
    device_id   TEXT NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    permission  TEXT NOT NULL,
    granted     BOOLEAN NOT NULL,
    at          TIMESTAMPTZ NOT NULL,              -- phone clock
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(), -- server clock
    PRIMARY KEY (device_id, permission, at)
);
