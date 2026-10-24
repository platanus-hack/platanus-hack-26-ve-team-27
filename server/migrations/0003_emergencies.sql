-- 0003_emergencies: offline emergencies from HTTP, SMS and the mesh (#57). See docs/adr/0003-offline-emergencies-store.md.
CREATE TABLE emergencies (
    emergency_id      TEXT PRIMARY KEY CHECK (emergency_id ~ '^[0-9a-f]{16}$'),
    device_id         TEXT NOT NULL REFERENCES devices (id),
    status            TEXT NOT NULL CHECK (status IN ('open', 'acked', 'cancelled')),
    seq               INTEGER NOT NULL CHECK (seq BETWEEN 0 AND 65535),  -- highest accepted
    sent_at           TIMESTAMPTZ NOT NULL,                              -- payload timestamp of the highest seq (phone clock)
    lat_e5            INTEGER CHECK (lat_e5 BETWEEN -9000000 AND 9000000),
    lon_e5            INTEGER CHECK (lon_e5 BETWEEN -18000000 AND 18000000),
    loc_age_s         INTEGER CHECK (loc_age_s BETWEEN 0 AND 65534),
    location_sent_at  TIMESTAMPTZ,                                       -- timestamp of the payload that carried the fix
    channels          TEXT[] NOT NULL CHECK (cardinality(channels) > 0 AND channels <@ ARRAY['http', 'sms', 'mesh']),
    min_hops          SMALLINT CHECK (min_hops BETWEEN 0 AND 255),       -- NULL = unknown / never via mesh
    first_received_at TIMESTAMPTZ NOT NULL,                              -- server clock
    updated_at        TIMESTAMPTZ NOT NULL,                              -- server clock, last receipt
    cancelled_at      TIMESTAMPTZ,
    CONSTRAINT emergencies_location_chk CHECK (
        (lat_e5 IS NULL) = (lon_e5 IS NULL)
        AND (lat_e5 IS NULL) = (loc_age_s IS NULL)
        AND (lat_e5 IS NULL) = (location_sent_at IS NULL)),
    CONSTRAINT emergencies_cancel_chk CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL))
);

CREATE INDEX emergencies_updated_at_idx ON emergencies (updated_at DESC);
