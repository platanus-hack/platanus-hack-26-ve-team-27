package intake

import (
	"sync/atomic"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// callWhenStill makes a still phone get the call branch instead of the
// immediate alert. main sets it once at boot from CALL_WHEN_STILL.
var callWhenStill atomic.Bool

// SetCallWhenStill sets whether a still phone (speed under
// contract.StillBelowMPS) gets BranchCall instead of BranchImmediateAlert.
// Off, the default, keeps the contract's three branches as they are; on, a
// person standing still (as on a stage) still gets the "Mamá" call. The fast
// branch and the unknown-speed rule do not change.
func SetCallWhenStill(on bool) { callWhenStill.Store(on) }

// chooseBranch is the contract's branch rule plus the CALL_WHEN_STILL switch.
func chooseBranch(ev contract.AlertEvent) contract.Branch {
	b := ev.Branch()
	if b == contract.BranchImmediateAlert && callWhenStill.Load() {
		return contract.BranchCall
	}
	return b
}
