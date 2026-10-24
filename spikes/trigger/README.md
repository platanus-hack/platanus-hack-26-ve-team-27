# Spike #10: locked-phone trigger on the demo Xiaomi

Goal: up-down-up on the volume keys with the phone locked, and a home-screen
widget, each reach a test endpoint with GPS. Results are in
[RESULTS.md](RESULTS.md) and in a comment on issue #10. #16 rebuilds this on
the frozen shapes or deletes it; it never ships as a mock.

What lives where (all spike code is in `app/src/main/java/com/team27/mama/spike/`):

| Piece | File |
| --- | --- |
| Up-down-up detector (pure, tested) | `VolumePattern.kt` |
| Dedupe of one press seen by both paths (pure, tested) | `PressDeduper.kt` |
| Both press paths feed one detector | `PressRouter.kt` |
| Keys with the screen on or the always-on display lit | `VolumePatternService.kt` (accessibility) |
| Keys with the panel fully off | `VolumeSessionService.kt` (silent media session) |
| Widget | `TriggerWidgetProvider.kt`, `res/layout/widget_trigger.xml` |
| What a trigger starts: AlertEvent, LocationUpdate, mic and camera probes | `TriggerSpikeService.kt`, `CameraProbe.kt`, `SpikeEvents.kt` |
| Evidence on the phone | `SpikeLog.kt` (`files/spike.log`) |
| Test endpoint, no database | `spikes/trigger/receiver.go` |

## Run it

```bash
# 1. Receiver (repo root). Appends one JSON line per request to spike-trigger.log.
go run ./spikes/trigger/receiver.go -log /tmp/spike-trigger.log    # :8089

# 2a. Over USB: the phone reaches the laptop through adb.
adb -s <serial> reverse tcp:8089 tcp:8089
./gradlew assembleDebug -Pmama.serverUrl=http://127.0.0.1:8089
# 2b. Unplugged (pocket test): a public tunnel instead.
cloudflared tunnel --url http://localhost:8089
./gradlew assembleDebug -Pmama.serverUrl=https://<name>.trycloudflare.com

adb -s <serial> install -r app/build/outputs/apk/debug/app-debug.apk
adb -s <serial> shell pm grant com.team27.mama android.permission.CAMERA   # setup (#15) does not ask for it

# 3. Turn the accessibility service on (or by hand: Settings > Accessibility >
#    MAMÁ, after Settings > Apps > MAMÁ > ⋮ > Allow restricted settings).
adb -s <serial> shell settings put secure enabled_accessibility_services \
  com.team27.mama/com.team27.mama.spike.VolumePatternService
adb -s <serial> shell settings put secure accessibility_enabled 1
```

Each trigger sends `POST /incidents` (AlertEvent with the best fix it has at
once), `POST /incidents/{id}/points` (a fresh fix) and `POST /spike/note` (a
spike-only line with the mic and camera results and fix ages). The phone adds
`X-Spike-Since-Trigger-Ms`, the time from the detected pattern to the request,
so latency does not depend on the phone and laptop clocks agreeing.

Read the evidence:

```bash
adb -s <serial> shell run-as com.team27.mama cat files/spike.log       # every key, source, screen/lock state, result
adb -s <serial> logcat -d | grep -E "MiuiInputKeyEventLog|MediaSessionService|DozeMachine"   # what the system saw
```
