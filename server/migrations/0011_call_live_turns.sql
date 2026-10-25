-- 0011_call_live_turns: what was said on a call while it lasted (#143), from Twilio's
-- real-time transcription: one row per final phrase of a track. inbound_track is the
-- person MAMÁ called, outbound_track is "Mamá". The report shows them until the post-call
-- webhook stores calls.transcript, which replaces them. They are provisional: never
-- judged by the duress detector, never sealed, never in the PDF.
-- (0010 is taken by #125.)
CREATE TABLE call_live_turns (
    call_id     BIGINT NOT NULL REFERENCES calls (id) ON DELETE CASCADE,
    track       TEXT NOT NULL CHECK (track IN ('inbound_track', 'outbound_track')),
    sequence_id INTEGER NOT NULL,     -- Twilio's SequenceId: ordered per track only
    message     TEXT NOT NULL,
    said_at     TIMESTAMPTZ NOT NULL, -- Twilio's Timestamp of the phrase: orders the two tracks
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (call_id, track, sequence_id)
);
