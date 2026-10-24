# Spike #11: "Mamá" calls a +58 phone

Goal: an ElevenLabs agent calls a real +58 phone through Twilio, and we know
how long it takes. If +58 fails, the browser (WebRTC) fallback works instead.
Numbers go in [RESULTS.md](RESULTS.md) and in a comment on issue #11.

What lives where:

| Piece | File |
| --- | --- |
| Outbound call client (`POST /v1/convai/twilio/outbound-call`) | `server/internal/call/client.go` |
| Post-call webhook `POST /webhooks/elevenlabs`, HMAC check | `server/internal/call/webhook.go` |
| Spike trigger `POST /spike/call` (only with `CALL_SPIKE_TOKEN`) | `server/internal/call/routes.go` |
| Timing script | `spikes/call/place_call.py` |
| WebRTC fallback page | `spikes/call/webrtc.html` |

## 1. One-time setup (dashboards)

1. **ElevenLabs API key.** Put `ELEVENLABS_API_KEY` in the main repo `.env`
   (it is not there yet). Scope it to Agents and give it a credit cap.
2. **Agent "hackathon"** (`ELEVENLABS_AGENT_ID`): the prompt and first
   message live in [`server/internal/call/agent/`](../../server/internal/call/agent/README.md);
   upload them with `cd server && go run ./cmd/pushagent` (it backs up the
   current configuration first), then **Publish**. Assign the agent to the
   imported Twilio number ("hackathon").
3. **Voice.** Until #35 lands, the agent's stock voice (Maggie) is used. To
   swap in Mariana's voice without touching the agent, set
   `ELEVENLABS_VOICE_ID` and turn on Agent > Security > Overrides > Voice;
   otherwise ElevenLabs rejects the override. Or just change the voice on the
   agent.
4. **Twilio.** Enable Venezuela in Console > Voice > Settings > Geo
   permissions. A **trial** account only calls Verified Caller IDs, plays a
   trial notice before the agent speaks (this adds to "first word"), and may
   only reach its signup country: if it refuses +58, upgrade or use the
   fallback.
5. **Post-call webhook.** ElevenLabs > Agents > Settings > Post-call webhook:
   URL `https://<public host>/webhooks/elevenlabs`, transcription only (leave
   audio off: the handler caps bodies at 10 MB). Copy the secret into
   `ELEVENLABS_WEBHOOK_SECRET`.

## 2. Public URL for the webhook

Either the Render deploy (`https://<mama-server>.onrender.com/webhooks/elevenlabs`,
after this branch is merged and the env vars are set in Render) or a tunnel
to a local server:

```bash
set -a; source /home/samu/Desktop/platanus-hack-26-ve-team-27/.env; set +a
cd server && go run ./cmd/server        # listens on :8080
cloudflared tunnel --url http://localhost:8080   # or: ngrok http 8080
```

Check it from outside: `curl -i -X POST https://<tunnel>/webhooks/elevenlabs -d '{"type":"ping"}'`
answers `401` (unsigned request) once the secret is set, `503` while it is not: the webhook refuses everything without a secret.

## 3. Place 3 calls and time them

```bash
set -a; source /home/samu/Desktop/platanus-hack-26-ve-team-27/.env; set +a
python3 spikes/call/place_call.py +58XXXXXXXXXX     # repeat 3 times
```

Answer the phone and press Enter on the laptop the moment you hear the
agent's first word. The script prints request, ringing and answer times from
Twilio's call status, your "first word" time, Twilio's price, ElevenLabs'
cost fields, and a ready-made row for RESULTS.md. Through the server instead:
`CALL_SPIKE_TOKEN=... python3 spikes/call/place_call.py +58... --via-server http://localhost:8080`. The route only dials `+58` numbers and keeps ElevenLabs' error text in the server log.

The server log shows the webhook arriving:

```
call webhook: conversation conv_… incident "spike-…" status "done" duration 31s turns 6 first agent turn at 0s
```

A refused or unanswered call logs `call_initiation_failure … reason "…"`
(when that webhook type is enabled), and Twilio errors 13227 / 32205 / 21215
mean the Venezuela geo permission is off.

Credits: if the script prints no cost fields, read the call in ElevenLabs >
Agents > Call history (credits per call) and Twilio > Monitor > Calls.

## 4. If +58 fails: WebRTC fallback

```bash
cd spikes/call && python3 -m http.server 8000
# open http://localhost:8000/webrtc.html?agent=<ELEVENLABS_AGENT_ID>
```

The microphone needs a secure origin: `localhost` works on the laptop; on a
phone, open the page through a tunnel (`cloudflared tunnel --url http://localhost:8000`).
The agent must be public (no authentication) for the widget to connect.
Record a short screen capture of the conversation as evidence.

## After the spike

The call engine (#18) keeps `call.Client` and `call.Webhook` (replacing
`Webhook.Received` with the code that stores the `calls` row) and deletes
`POST /spike/call`, `CALL_SPIKE_TOKEN` and this folder.
