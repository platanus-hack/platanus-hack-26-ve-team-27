package intake

import (
	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// chooseBranch picks the branch from how the alert was started, not from the
// speed (product decision, 24 Oct): the home-screen widget alerts the family
// at once with no call, and every other trigger always gets the "Mamá" call,
// whether the phone is still, walking, in a car or has no speed. The points'
// speed is still stored on the incident.
func chooseBranch(ev contract.AlertEvent) contract.Branch {
	if ev.Trigger == contract.TriggerWidget {
		return contract.BranchImmediateAlert
	}
	return contract.BranchCall
}
