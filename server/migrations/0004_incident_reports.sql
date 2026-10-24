-- 0004_incident_reports: the incident report page, its call recordings and
-- its seal (#76). See docs/adr/0005-incident-report-seal.md.

-- The private link to an incident's report (GET /i/{token}). token is 128
-- random bits in base64url without padding, unrelated to the incident_id the
-- phone made. One link per incident; an expired one is replaced the next time
-- a link is asked for.
CREATE TABLE incident_reports (
    incident_id UUID PRIMARY KEY REFERENCES incidents (id) ON DELETE CASCADE,
    token       TEXT NOT NULL UNIQUE CHECK (token ~ '^[A-Za-z0-9_-]{22}$'),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

-- The recording of a call, fetched from ElevenLabs after its post-call
-- webhook. In Postgres because Render's disk is ephemeral; a minute of call is
-- about 1 MB. sha256 is of audio, as served.
CREATE TABLE call_recordings (
    call_id      BIGINT PRIMARY KEY REFERENCES calls (id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,
    audio        BYTEA NOT NULL,
    sha256       BYTEA NOT NULL CHECK (octet_length(sha256) = 32),
    stored_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The seal: document holds the exact bytes of ficha.json, and signature the
-- server's Ed25519 signature over 'MAMA-incident-report-v1' || document, by
-- public_key. Written once per incident.
CREATE TABLE incident_seals (
    incident_id UUID PRIMARY KEY REFERENCES incidents (id) ON DELETE CASCADE,
    sealed_at   TIMESTAMPTZ NOT NULL,
    document    BYTEA NOT NULL,
    signature   BYTEA NOT NULL CHECK (octet_length(signature) = 64),
    public_key  BYTEA NOT NULL CHECK (octet_length(public_key) = 32)
);

-- What Twilio's call status said while the call engine polled it: when the
-- phone first rang and when the call was answered (server clock). Written
-- only when the incident report is on; Twilio's own clock is not used.
CREATE TABLE call_progress (
    call_id       BIGINT PRIMARY KEY REFERENCES calls (id) ON DELETE CASCADE,
    twilio_status TEXT NOT NULL,
    ringing_at    TIMESTAMPTZ,
    answered_at   TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL
);
