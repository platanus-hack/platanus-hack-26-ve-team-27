# Home: hold Mamá to call

Branch: `feat/home-hold-to-call`, stacked on `fix/setup-flow-cancel-permissions` (PR #148, which makes "Fue sin querer" cancel the incident on the server). Rebase onto `main` once #148 merges.

## Objective

Add a home screen to the app, based on variant A of the throwaway prototype (artifact "Prototipo inicio MAMÁ"). Mamá's portrait is the trigger: hold it for 1.5 s while a ring fills. The screen then animates the "processing" phase and the "listening" phase in place. A "Fue sin querer" cancel button is always visible while an incident is active.

## Problem / why

- After setup the user lands on Setup's done state. There is no home and no in-app way to trigger.
- `TriggerService.start(context, Trigger.IN_APP)` already exists. Its KDoc reserves it for an in-app button, but nothing calls it.
- The online trigger path publishes no observable progress. The UI cannot tell "sending" from "sent", and cannot tell when the service ignored the start (setup incomplete, no location permission, or an incident already running).

## Scope

- Pure Kotlin home model: hold timer → requested → processing → listening → cancelled/failed → idle. Unit tested.
- A small `StateFlow` of online trigger progress published by `TriggerService`, so Home shows real phases instead of timers.
  - Processing lasts while sending, with a minimum on-screen time so the animation reads.
  - Listening starts once the alert is accepted or the offline fallback takes over.
  - Failed: the service never started within a timeout.
- An `IN_APP` trigger does not open `TriggerConfirmation` through the handoff, because Home already shows the progress. Widget and volume triggers are unchanged.
- `HomeScreen`, built to `app/UI_RULES.md`. `Route.Home`. The start destination becomes Home once setup is completed. Setup's done "Continuar" goes to Home.
- Animations, following the prototype:
  - Arming: the ring fills while held. Releasing early cancels the hold.
  - Processing: a spinning arc around the portrait (`MamaPose.Calling`, never `Typing`, per the bystander rule) and three mini step bars.
  - Listening: `MamaPose.Listening`, ripples, a radial voice-bar halo, and a scrolling voice-memo waveform under the portrait (the user asked for the "second listening" movement). Plus an elapsed timer.
  - All of it is static when system animations are off.

## Constraints

- `app/UI_RULES.md`: theme tokens only, Material3 only, Spanish copy with "tú", neutral bystander-safe words, no red except errors, one primary action, undo as `TextButton`.
- No microphone recording exists. The waveform is decorative and is not tied to audio. Real amplitude is out of scope.
- No Compose UI test infrastructure exists. Logic goes into pure Kotlin with JUnit4 tests.
- TDD: off (no project/session config). Runner: `./gradlew testDebugUnitTest`. Full gate: `scripts/build.sh`.
- Do not touch `mesh/`.

## Tasks

- [x] T1 Trigger progress flow in `TriggerService` (+ IN_APP skips the confirmation handoff), with tests. Route: delegated writer.
- [x] T2 Pure home model (hold → phases, failure timeout, min processing time), with tests. Route: delegated writer.
- [x] T3 `HomeScreen` with the animations, `Route.Home`, start-destination and Setup "Continuar" wiring, `@Preview`. Route: delegated writer.
- [x] T4 Docs: `app/UI_RULES.md` gets a Home entry. Record commits here. Route: delegated writer.
- [x] T5 Fix the two advisory review findings, with tests. (1) `R3-cancel-readopt`: Cancelled must not re-adopt the cancelled incident while progress is still Sending or Active. (2) `R3-stale-sawActive-on-adopt`: `adopt()` must reset `sawStarted`/`sawActive`. Reason: (1) breaks the acceptance line "Fue sin querer ... then returns to idle". Route: delegated writer.

Route evidence: the writer trigger fires (4+ non-trivial files across trigger, ui and navigation), so all four tasks go to one bounded writer.

## Acceptance

- Holding the portrait 1.5 s triggers once (`Trigger.IN_APP`). Releasing earlier does nothing.
- Processing and listening reflect the real service progress. "Fue sin querer" calls `emergency?.cancel()` and `TriggerService.cancel(context)`, then returns to idle.
- A start the service ignores ends in a readable failure line, not an endless spinner.
- `scripts/build.sh` is green. The UI_RULES self-check grep prints nothing.

## Progress

- T1 done (`b1d7fd0`): `TriggerService.progress` (`StateFlow<TriggerProgress>`: Idle, Sending, Active) in the service companion, so nothing else is wired and it resets in `onDestroy`. IN_APP skips the confirmation handoff (`opensConfirmation`).
- T2 done (`4299d2d`): `ui/home/HomeModel.kt` + `HomeModelTest` (fake clock).
- T3 done (`e2939c7`): `HomeScreen`, `HomeVisuals`, `Route.Home`, start destination, Setup "Continuar" to Home. The model also adopts an incident started elsewhere (widget, volume, recreated screen). The "En línea" / "Ubicación activa" chips were dropped: nothing backs them honestly.
- T4 done: `app/UI_RULES.md` Home entry.
- Route: one delegated writer, as planned. Verification: `scripts/build.sh` green; UI_RULES self-check grep prints nothing.
- Not done: no on-device run (no phone in this environment).
- T5 done (`ce1eb48`): a cancel now blocks adopt until progress is seen idle (a stale Sending/Active no longer revives the cancelled incident), and adopt() sets sawStarted/sawActive to match what it adopts.
