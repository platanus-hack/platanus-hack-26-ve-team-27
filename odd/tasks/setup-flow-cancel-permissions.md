# Setup flow, false-alarm cancel and permission retry fixes

## Objective
Fix three bugs found while testing on two phones (Samsung A55, Realme RMX3081) against the Render server.

## Problem
1. After setup finishes (contact saved + phone registered), the done screen only offers "edit data"; there is no way to continue into the app.
2. During an alert started from the widget, the "Me equivoqué" (false alarm) option neither cancels the call/alert nor leaves the alert screen.
3. In the setup permission steps, once a permission step was shown (e.g. accessibility) it cannot be opened again; the only way to retry was clearing app data.

## Scope / constraints
- App only (`app/`). Follow `app/UI_RULES.md` (one theme file, Material3 only, Spanish copy).
- Server contract unchanged unless the cancel bug proves to be server-side (report back first).
- TDD: off (no project/session config). Runner: `./gradlew testDebugUnitTest`.

## Tasks
- [x] T1 Setup done screen: add a primary "Continuar" action that leaves setup into the app's main screen.
- [x] T2 False-alarm cancel: "Me equivoqué" cancels the active alert (server + local service/call) and dismisses the alert UI.
- [x] T3 Permission steps: any permission/system-screen step can be re-opened after it was shown, skipped or denied (re-request, or deep-link to settings when the system won't show the dialog again).

## Acceptance criteria
- T1: after both tasks are DONE, the user reaches the main screen with one tap; "edit data" still available.
- T2: tapping "Me equivoqué" stops location upload/foreground service, cancels on the server, and closes the alert screen.
- T3: tapping a previously shown step (accessibility included) opens its dialog or settings screen again.

## Checks
- `./gradlew :app:testDebugUnitTest :app:assembleDebug`
- Manual on both phones.

## Route
Delegated direct (writer trigger: 3 non-trivial areas across several files).

## Progress
- Branch `fix/setup-flow-cancel-permissions` created from `main` at `ed1f82c`.
- T1 0086827 (route: delegated direct), T2 0c7ba74, T3 09f86b0.
- T2 gap: the server has no cancel endpoint for online incidents (POST /incidents only); app-side stop done, server-side cancel/call cancel not possible without a server change.
