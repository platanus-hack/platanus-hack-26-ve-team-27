# Spike #11 results

Fill in from `place_call.py` output. Leave a cell blank rather than guess.

Setup: Twilio account type (trial / paid): paid (Full) · Voice (stock Maggie / Mariana #35): stock agent voice `9Ux14cxOYULytGFBVsW4` (#35 still open) · Webhook URL: `https://museum-compact-amd-reload.trycloudflare.com/webhooks/elevenlabs` (cloudflared quick tunnel to a local server)

Date: 2026-10-24. All calls to the same +58 mobile (Movistar, 0424), placed straight to ElevenLabs (`place_call.py` without `--via-server`).

| Time (VET) | To | Twilio final status | Request → accepted (ms) | Request → ringing (ms) | Answer → first word (ms) | Credits (ElevenLabs) | Conversation ID |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 09:59 | +584…34 | completed (97 s) | 625 | 5094 | < 1000 * | 1464 | conv_8801m5q375zgf3m92zrpg96pyw39 |
| 10:09 | +584…34 | completed (90 s) | 1036 | 6170 | < 1000 * | 1215 | conv_6701m5q3tt9sexx8xgsypqymard3 |
| 10:12 | +584…34 | completed (83 s) | 570 | 4387 | < 1000 * | 1198 | conv_8901m5q4013ke0rseypp5xfa3vg2 |
| 10:20 | +584…34 | completed (86 s) | 693 | 4800 | < 1000 * | 1211 | conv_1201m5q4ecw5fpmbmvxrakhagsrh |
| 10:31 | +584…34 | completed (60 s) | 654 | 2263 | < 1000 * | 878 | conv_4001m5q53etgeddshra4fk8bbx7s |

Request → answered (ms): 10898, 12243, 12458, 10898, 8533. That includes the time a person takes to pick up.

\* The Enter stopwatch was not pressed, so "first word" comes from the ElevenLabs transcript: in all 5 calls the agent's first turn is at `time_in_call_secs` 0 and the callee replies at 2–3 s. The transcript only has whole seconds, so this is "under a second", not a millisecond value.

Averages: request → ringing **4.5 s** (2.3–6.2 s); credits **1193 per call** (~$0.12, about 1.4 min each: ~900 voice + ~260 LLM). Twilio bills **$0.1739 per started minute** to this +58 mobile ($0.3478 for calls 1–4, $0.1739 for call 5). In total, about $0.30–0.47 per 1–1.5 min call.

- Rang and the agent spoke in the expected voice: yes. The phone rang and the agent spoke on all 5 calls, in the stock voice. Mariana's voice is swapped in through `ELEVENLABS_VOICE_ID` once #35 lands.
- Transcript webhook arrived at the public URL (server log line): yes, call 5:
  `2026/10/24 10:31:31 call webhook: conversation conv_4001m5q53etgeddshra4fk8bbx7s incident "spike-1792852212" status "done" duration 61s turns 20 first agent turn at 0s`
  Calls 1–4 did not reach the handler (server down, a stale webhook secret, then a closed tunnel). These were setup mistakes, not code faults.
- If +58 failed: not needed. +58 worked on the paid Twilio account, so the WebRTC fallback (`webrtc.html`) was not exercised.
