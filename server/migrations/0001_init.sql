-- 0001_init: the contract schema (#9). See docs/contract.md.
-- Enum values mirror server/internal/contract; change both together.

-- One row per installed app. id is the device ID the app generates at
-- install (no auth). Upserted by every DeviceRegistration.
CREATE TABLE devices (
    id             TEXT PRIMARY KEY,
    phone_number   TEXT NOT NULL,                 -- E.164; "Mamá" calls it
    family_numbers TEXT[] NOT NULL DEFAULT '{}',  -- E.164; the notifier writes to them
    duress_phrase  TEXT NOT NULL,
    app_version    TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per alert. id is the incident_id the phone generates, so a retried
-- AlertEvent hits the primary key instead of opening a second incident.
CREATE TABLE incidents (
    id           UUID PRIMARY KEY,
    device_id    TEXT NOT NULL REFERENCES devices (id),
    trigger      TEXT NOT NULL CHECK (trigger IN ('volume_pattern', 'widget', 'tile', 'in_app')),
    branch       TEXT NOT NULL CHECK (branch IN ('silent_tracker', 'immediate_alert', 'call')),
    speed_mps    DOUBLE PRECISION,                -- the speed the branch was picked from; NULL = unknown
    has_audio    BOOLEAN NOT NULL DEFAULT false,
    triggered_at TIMESTAMPTZ NOT NULL,            -- phone clock
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    notified_at  TIMESTAMPTZ                      -- set once the family message went out (one per incident)
);
CREATE INDEX incidents_device_id_created_at_idx ON incidents (device_id, created_at DESC);

-- GPS fixes of an incident, from the AlertEvent and later LocationUpdates.
-- The unique key makes a resent fix a no-op (ON CONFLICT DO NOTHING).
CREATE TABLE gps_points (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    incident_id UUID NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    lat         DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng         DOUBLE PRECISION NOT NULL CHECK (lng BETWEEN -180 AND 180),
    accuracy_m  DOUBLE PRECISION,
    speed_mps   DOUBLE PRECISION,
    recorded_at TIMESTAMPTZ NOT NULL,             -- phone clock
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (incident_id, recorded_at)
);

-- Outbound "Mamá" calls. An incident can have more than one (a retry), so
-- incident_id is not unique. conversation_id is the ElevenLabs id; it is
-- unique so a repeated webhook updates the same row. The verdict lives here:
-- the detector judges one call's transcript.
CREATE TABLE calls (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    incident_id     UUID NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    conversation_id TEXT UNIQUE,                  -- NULL until ElevenLabs answers the outbound request
    call_sid        TEXT,                         -- Twilio CallSid, when known
    status          TEXT NOT NULL DEFAULT 'requested'
                    CHECK (status IN ('requested', 'in_progress', 'done', 'failed')),
    transcript      JSONB,                        -- []TranscriptTurn from the webhook
    duration_secs   INTEGER,
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at        TIMESTAMPTZ,
    verdict         TEXT CHECK (verdict IN ('duress', 'no_duress', 'unclear')),
    verdict_reason  TEXT,
    verdict_at      TIMESTAMPTZ
);
CREATE INDEX calls_incident_id_idx ON calls (incident_id);
