package intake

import (
	"context"
	"log"
	"time"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// Incident is what intake hands to the call engine or the notifier once a new
// incident is stored. Location is the newest fix of the AlertEvent.
type Incident struct {
	ID          string
	DeviceID    string
	Branch      contract.Branch
	SpeedMPS    *float64 // the speed the branch was picked from; nil = unknown
	Location    contract.Point
	TriggeredAt time.Time
}

// CallStarter places the "Mamá" call for a BranchCall incident (#18).
type CallStarter interface {
	StartCall(ctx context.Context, inc Incident)
}

// CallCanceller stops the "Mamá" call of an incident the person called off:
// it hangs up a ringing or live call and places no retry. It must be safe to
// call again for the same incident.
type CallCanceller interface {
	CancelCall(ctx context.Context, incidentID string)
}

// Notifier alerts the family for a BranchImmediateAlert or
// BranchSilentTracker incident (#20).
type Notifier interface {
	Notify(ctx context.Context, inc Incident)
}

// LogHandoff is the default CallStarter and Notifier until #18 and #20 plug
// in real ones: it only logs the handoff.
type LogHandoff struct{}

var (
	_ CallStarter = LogHandoff{}
	_ Notifier    = LogHandoff{}
)

func (LogHandoff) StartCall(_ context.Context, inc Incident) {
	log.Printf("intake: incident %s branch %s: call engine not wired yet", inc.ID, inc.Branch)
}

func (LogHandoff) Notify(_ context.Context, inc Incident) {
	log.Printf("intake: incident %s branch %s: notifier not wired yet", inc.ID, inc.Branch)
}
