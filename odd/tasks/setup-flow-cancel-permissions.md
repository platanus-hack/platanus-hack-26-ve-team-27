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
- [x] T4 Mamá contact photo: saving the contact also sets a photo (illustration `mama_lista`, full-size display photo), replaced on update.
- [x] T5 Cancel endpoint: server route to cancel an online incident (stop the pending/ringing call, mark incident cancelled), contract docs, app client call from "Fue sin querer".
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
- T4 a4f60f2 (delegated direct): Photo thumbnail row (update or insert, never two) plus full-size `RawContacts.DisplayPhoto`; failure only logged.
- T5 server 4bc4606: `POST /incidents/{id}/cancel`, migration 0010 `incidents.cancelled_at`, Twilio hang-up, retries and fallback stopped, family claim refused when cancelled, contract docs and sample `incident_cancel.json`.
- T5 app cd8f022: `TriggerService.cancel` POSTs the cancel from a scope that outlives the service (3 attempts, best effort).
- T2 gap closed by T5. Gaps: auth is device_id only, like `/points`; a cancel before the alert reached the server answers 404 and the alert may still land.
