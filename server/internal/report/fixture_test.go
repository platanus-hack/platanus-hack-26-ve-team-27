package report

import (
	"crypto/ed25519"
	"crypto/sha256"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

const (
	testIncidentID = "9b2e4f1d-3c5a-4e7b-8d6f-1a2b3c4d5e6f"
	testPhrase     = "dile a la tía Carmen que llego tarde"
	testToken      = "AAAAAAAAAAAAAAAAAAAAAA"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr[T any](v T) *T { return &v }

// testTranscript is a 61 s call where the user says the phrase at 0:15.
func testTranscript() []contract.TranscriptTurn {
	return []contract.TranscriptTurn{
		{Role: "agent", Message: "¿Aló? Mi amor, ¿dónde estás?", TimeInCallSecs: 0},
		{Role: "user", Message: "Hola mami, voy saliendo del trabajo.", TimeInCallSecs: 4},
		{Role: "agent", Message: "¿Y ya comiste algo?", TimeInCallSecs: 9.5},
		{Role: "user", Message: "Sí, mami. Oye, dile a la tía Carmen que llego tarde.", TimeInCallSecs: 15},
		{Role: "agent", Message: "Va, mi amor, yo le digo. Cuídate.", TimeInCallSecs: 22},
	}
}

// sealedRecord is a call-branch incident with every event of the #76 table:
// three points, one answered call with the phrase, a verdict, the family
// message, the recording and the seal. Times are UTC; VET is UTC-4.
func sealedRecord() Record {
	audio := sha256.Sum256([]byte("fake mp3"))
	rec := Record{
		Incident: Incident{
			ID:          testIncidentID,
			DeviceID:    "3f1c9a52-7b1e-4c8a-9a0e-2d6b8f4e1a77",
			Trigger:     contract.TriggerVolumePattern,
			Branch:      contract.BranchCall,
			SpeedMPS:    ptr(1.1),
			TriggeredAt: at("2026-10-24T13:10:00.253Z"),
			CreatedAt:   at("2026-10-24T13:10:00.653Z"),
			NotifiedAt:  ptr(at("2026-10-24T13:11:08Z")),
		},
		Device: Device{PhoneNumber: "+584141234567", FamilyNumbers: 2, DuressPhrase: testPhrase},
		Points: []Point{
			{Lat: 10.4961, Lng: -66.8531, AccuracyM: ptr(12.5), SpeedMPS: ptr(1.1), RecordedAt: at("2026-10-24T13:10:00Z"), ReceivedAt: at("2026-10-24T13:10:00.650Z")},
			{Lat: 10.4975, Lng: -66.8502, AccuracyM: ptr(8.0), SpeedMPS: ptr(1.3), RecordedAt: at("2026-10-24T13:10:45Z"), ReceivedAt: at("2026-10-24T13:10:46Z")},
			{Lat: 10.4989, Lng: -66.8477, AccuracyM: ptr(9.5), RecordedAt: at("2026-10-24T13:11:30Z"), ReceivedAt: at("2026-10-24T13:11:31Z")},
		},
		Calls: []Call{{
			ID:             1,
			ConversationID: "conv_01",
			Status:         "done",
			Transcript:     testTranscript(),
			DurationSecs:   ptr(61),
			RequestedAt:    at("2026-10-24T13:10:01.200Z"),
			EndedAt:        ptr(at("2026-10-24T13:11:05Z")),
			Verdict:        contract.VerdictDuress,
			VerdictReason:  "the user said the duress phrase",
			VerdictAt:      ptr(at("2026-10-24T13:11:07Z")),
			Recording:      &Recording{ContentType: "audio/mpeg", SHA256: audio, Size: 993280, StoredAt: at("2026-10-24T13:11:20Z")},
		}},
	}
	key := testKey()
	sealedAt := at("2026-10-24T13:11:25Z")
	doc, err := Document(rec, sealedAt, key.Public().(ed25519.PublicKey))
	if err != nil {
		panic(err)
	}
	rec.Seal = &Seal{SealedAt: sealedAt, Document: doc, Signature: Sign(doc, key), PublicKey: key.Public().(ed25519.PublicKey)}
	return rec
}

// testKey is a fixed server key for tests (seed 0x01 x 32).
func testKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = 1
	}
	return ed25519.NewKeyFromSeed(seed)
}

// openRecord is the same incident before the recording and the seal.
func openRecord() Record {
	rec := sealedRecord()
	rec.Calls[0].Recording = nil
	rec.Seal = nil
	return rec
}
