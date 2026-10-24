# Spike #10 results: locked-phone trigger on the demo Xiaomi

Run on Sat Oct 24, 2026, 11:05–12:30 VET, after the 05:05 VET due time. Every
press below is a physical press by a person unless it says **simulated**.
Evidence: the phone's `files/spike.log`, the receiver log
(`spikes/trigger/receiver.go`, 32 alerts) and `adb logcat`
(`MiuiInputKeyEventLog`, `MediaSessionService`, `DozeMachine`).

## The phone

| | |
| --- | --- |
| Model | POCO X8 Pro Max, `2602BPC18G` (serial `BUPV89TORKTKGQDA`) |
| Android | 16 (SDK 36), security patch 2026-08-01 |
| HyperOS | 3.0, build `OS3.0.303.0.WPLMIXM` |
| Setup | #15 debug build, all runtime permissions, autostart on, battery "Sin restricciones"; CAMERA granted by `adb pm grant` for the video probe; accessibility turned on through `adb settings put secure` (the by-hand "Allow restricted settings" path was not walked) |

## Acceptance criteria

| Criterion | Result | Evidence |
| --- | --- | --- |
| Under 2 s from the pattern to the server with GPS, after 30 min locked in a pocket | **fail / not run as written** | The 30 min run was not done (the tester declined the wait). Unplugged, about 1.5 min in a pocket, over mobile network and a Cloudflare tunnel: pattern 16:27:56.098, received 16:28:00.354, **4.26 s**. The phone sent at +69 ms; the first HTTPS request took 4378 ms (cold radio, DNS, TLS through the tunnel), the next one 830 ms. Its point was a 230 s old fused fix at 100 m, not a fresh GPS fix. Over USB (`adb reverse`), locked, screen off: received 54–209 ms after the pattern (phone clock ~78 ms behind the laptop, so ~0–130 ms real), phone-side 10–71 ms. |
| Survives clearing recents and a reboot, and comes back on its own | **recents: pass; reboot: not run** | After clearing recents the service reconnected by itself (`15:12:56 a11y connected`) and a locked, screen-off pattern reached the server (`15:13:09 … status=201`). Every `adb install -r` also killed the app and both services came back within 6 s. No reboot was done (`/proc/uptime` 20 days at 12:17). |
| Mic starts from the trigger: yes or no | **yes** | A foreground service of type `location\|microphone\|camera` starts from the locked trigger; 1 s of `AudioRecord` gets real audio every time (`peak=1667…16556`, `clientSilenced=false`), including locked with the panel off. The spike never uploads audio (`has_audio: false`). |
| The pattern doesn't open the camera | **pass** | No camera or any UI opened on any trigger (tester, and the log shows no activity starts). Caveat: HyperOS's own volume-key camera shortcut is off on this phone (`volumekey_launch_camera=0`); up-down-up has a single down, so the down-down shortcut would not match anyway, but it was not tried with the shortcut on. |
| The widget triggers the same event | **pass** | Widget tap at 15:09:40: `trigger: widget`, AlertEvent 201, sent 20 ms after the service started, received 110 ms wall (USB). Only tested unlocked, screen on. |
| Android and HyperOS versions noted | **pass** | Above. |

## Findings #16 needs

1. **Accessibility alone does not work in a pocket.** It only gets keys
   HyperOS passes to the user: screen on, or the always-on display lit. With
   the panel fully off it got 3 of 9 clean presses, and each hit matched a
   `DozeMachine … DOZE_AOD` window exactly (11:33:01, 11:34:16, 11:36:35;
   at 11:35:08 only the up after the AOD lit arrived).
2. **A silent media session fixes it.** With the panel off, Android sends
   volume keys to the media button session. `VolumeSessionService` keeps a
   remote-playback `MediaSession` with a `VolumeProvider`, and both paths feed
   one detector with a dedupe. Two gotchas, each cost a round on the phone:
   - Android 16 routes keys to the session only if the app really plays
     audio (`Media button session is com.spotify.music` until then), so it
     loops one second of zeros through an `AudioTrack` (0 of 6 without it).
   - `MediaSession` drops `VolumeProvider` callbacks unless `setCallback` is
     called (the log showed `Adjusting com.team27.mama/mama-volume by 1` but
     nothing arrived).

   After both: **8 of 8** physical patterns locked with the panel fully off
   (12:05:57–12:07:22 and 12:16:09, 12:16:30), each sent 23–42 ms after the
   pattern. Music in Spotify kept playing; no player showed on the lock
   screen.
3. **Side effects of the session**, to weigh in #16: with the panel off the
   volume keys no longer change the media volume (they go to MAMÁ); headset
   media buttons go to MAMÁ while it holds the session (a Bluetooth pause
   reached it at 12:01:22); it keeps an audio track open (battery not
   measured).
4. **Latency is the network, not the trigger.** Pattern to request is under
   75 ms on every run. The pocket run lost 4.3 s on the first HTTPS request.
   For #16: keep a warm connection or retry fast, and measure against the
   real server URL (the tunnel adds hops).
5. **The first point is often not GPS.** `getLastKnownLocation` gives an old
   or coarse fix (up to 230 s old; one was 4062 m at 15:33:02). The fresh GPS
   fix (1.2–8.8 m when the sky is visible) usually comes back as the cached
   one. #16 should send at once and follow with a fresh fix
   (`LocationUpdate`), as the spike does.
6. **Press timing.** Blind presses took up to 1.28 s between keys (the
   first pocket attempt missed with a 1 s limit; now 1.5 s). Twice the
   tester pressed up and down together (11:19:52, 11:20:07), which reads as
   up, down and no third press.
7. **Video works with nothing on screen.** A `camera` foreground service
   records back-camera video straight to a file with Camera2 + MediaRecorder,
   locked with the panel off: opens in 52–270 ms, 3 s clips (1280x720, 81
   frames, checked by extracting frames). Not in the contract (no video
   field); sending it needs the Tech lead's OK. Android shows its green
   camera dot while recording.
8. The foreground-service notifications ("MAMÁ · Sincronizando", "Activa")
   are low importance and HyperOS folds them into silent notifications; the
   tester did not see them.
9. `adb shell input keyevent` never reaches the accessibility path on this
   phone; it does reach the media session. One such **simulated** run
   (16:04:51, panel off) only checked the routing and is not counted above.
