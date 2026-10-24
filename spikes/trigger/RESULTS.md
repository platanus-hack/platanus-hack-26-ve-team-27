# Spike #10 results: locked-phone trigger on the demo Xiaomi

Run on Sat Oct 24, 2026, 11:05–13:20 VET, after the issue's 05:05 VET due time.
All times are VET (UTC-4); `spike.log` itself writes UTC. Every
press below is a physical press by a person unless it says **simulated**.
Evidence: the phone's `files/spike.log`, the receiver log
(`spikes/trigger/receiver.go`) and `adb logcat`
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
| Under 2 s from the pattern to the server with GPS, after 30 min locked in a pocket | **pass on mobile data after 2 min; 30 min not run; fails on the venue Wi-Fi** | Unplugged, in a pocket, through a Cloudflare tunnel, with the warm-up (finding 4). **Mobile data (Movistar LTE), ~2 min in the pocket, 13:18:01:** received **179 ms** after the pattern (phone clock 51 ms ahead, so ~130 ms real); first point a fused fix at 13 m, ~1 s old. **Venue Wi-Fi "Hackathon" (−68 dBm on a table), ~9 min in the pocket, 13:11:59:** received **20.4 s** after the pattern; the warm-up GET alone took 4.8 s and the alert request 20 s; point 15 m. Before the warm-up, on the same Wi-Fi (~1.5 min, 12:27:56): 4.26 s and a 230 s old 100 m point. The 30 min run was not done (the tester declined the wait). Over USB (`adb reverse`), locked, panel off: 54–209 ms. |
| Survives clearing recents and a reboot, and comes back on its own | **recents: pass; reboot: not run** | After clearing recents the service reconnected by itself (`a11y connected` at 11:12:56) and a locked, screen-off pattern reached the server (`status=201` at 11:13:09). Every `adb install -r` also killed the app and both services came back within 6 s. No reboot was done (`/proc/uptime` 20 days at 12:17). |
| Mic starts from the trigger: yes or no | **yes** | A foreground service of type `location\|microphone\|camera` starts from the locked trigger; 1 s of `AudioRecord` gets real audio every time (`peak=1667…16556`, `clientSilenced=false`), including locked with the panel off. The spike never uploads audio (`has_audio: false`). |
| The pattern doesn't open the camera | **pass** | No camera or any UI opened on any trigger (tester, and the log shows no activity starts). Caveat: HyperOS's own volume-key camera shortcut is off on this phone (`volumekey_launch_camera=0`); up-down-up has a single down, so the down-down shortcut would not match anyway, but it was not tried with the shortcut on. |
| The widget triggers the same event | **pass (unlocked only)** | Widget tap at 11:09:40: `trigger: widget`, AlertEvent 201, sent 20 ms after the service started, received 110 ms wall (USB). Only tested unlocked with the screen on; a widget is not reachable on a locked phone anyway. |
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
   75 ms on every run (855 ms when it waits for a fix, finding 5). What helps:
   `Warmup` opens a keep-alive connection (`GET /`) and starts the GPS on the
   first press, so the ~1–2 s of the other two presses pays the cold radio,
   DNS and TLS; `SpikeHttp` reads and closes responses instead of
   `disconnect()` so the alert reuses that connection. On mobile data the
   warm-up finished (1.04 s) before the third press and the alert took 334 ms.
   What does not: a weak, crowded Wi-Fi, where one request took 20 s. For
   #16: do not rely on one HTTPS request; send the SMS channel (#58) in
   parallel, retry on a fresh connection after ~2 s, and measure against the
   real server URL (the tunnel adds hops).
5. **The first point is often not GPS.** `getLastKnownLocation` gives an old
   or coarse fix (up to 230 s old; one was 4062 m at 11:33:02). The spike now
   sends the last known fix at once only if it is at most 30 s old and 50 m;
   otherwise it waits up to 1 s for a fresh one, then sends what it has, and
   follows with the first fix within 20 m as a `LocationUpdate` (a warmed-up
   GPS gave 1.0 m about 5 s after the pattern). With the warm-up, the pocket
   runs sent 13–15 m fused fixes about 1 s old.
6. **Press timing.** Blind presses took up to 1.28 s between keys (the
   first pocket attempt missed with a 1 s limit; now 1.5 s). Twice the
   tester pressed up and down together (11:19:52, 11:20:07), which reads as
   up, down and no third press.
7. **Video works with nothing on screen.** A `camera` foreground service
   records back-camera video straight to a file with Camera2 + MediaRecorder,
   locked with the panel off: opens in 52–270 ms, 3 s clips (1280x720, 81
   frames, checked by extracting frames). Uploaded to a spike-only receiver
   route (`POST /spike/video`): 1.04 MB in 1.3 s on mobile data, 1.29 MB in
   5.2 s on the venue Wi-Fi, after the alert. Not in the contract (no video
   field); sending it to the real server needs the Tech lead's OK. Android
   shows its green camera dot while recording.
8. The foreground-service notifications ("MAMÁ · Sincronizando", "Activa")
   are low importance and HyperOS folds them into silent notifications; the
   tester did not see them.
9. `adb shell input keyevent` never reaches the accessibility path on this
   phone; it does reach the media session. One such **simulated** run
   (12:04:51, panel off) only checked the routing and is not counted above.
