# Mamá agent (ElevenLabs)

The prompt and first message of the "Mamá" agent live here, not only in the
ElevenLabs dashboard:

| File | What |
| --- | --- |
| `prompt.md` | System prompt (`conversation_config.agent.prompt.prompt`) |
| `first_message.txt` | First message (`conversation_config.agent.first_message`) |
| `agent.go` | Embeds both and builds the PATCH body: prompt, first message, language `es`, system tool `end_call` |

Both texts use three dynamic variables, which the call engine sends when it
places the call (contract ticket #104): `{{nombre}}`, `{{frase_alerta}}` and
`{{frase_segura}}`. `frase_segura` may be an empty string: then only the
alert phrase counts. ElevenLabs refuses to start a conversation when a
variable the agent uses is missing, so every caller must send all three.

How Mamá talks: she greets by name and asks "¿Cómo vas, mi amor?".
- Safe phrase: she says goodbye quickly.
- Alert phrase (or close to it): same tone, "Ah, ok, yo le digo", then up to
  5 everyday questions, one at a time (where, with whom, how many, car or
  walking, where to, all calm), then a normal goodbye.
- Neither: she asks once more, then two questions (where, with whom) and
  says goodbye.

She never says the phrases, never says policía, emergencia, alerta, peligro,
ayuda, "¿estás bien?" or "¿te están obligando?", and hangs up with `end_call`
in under 90 seconds. `agent_test.go` checks the variables and that no example
line in the prompt uses a forbidden word.

## Upload

Edit `prompt.md` or `first_message.txt`, then:

```bash
set -a; source <main repo>/.env; set +a   # ELEVENLABS_API_KEY, ELEVENLABS_AGENT_ID
cd server
go run ./cmd/pushagent --dry-run          # print the body, send nothing
go run ./cmd/pushagent                    # back up, then PATCH the agent
```

Before writing, the command saves the agent's current configuration (the
`GET /v1/convai/agents/{agent_id}` answer) to
`server/cmd/pushagent/backup/<agent_id>-<UTC time>.json` (gitignored). To go
back, send that backup's agent section again:

```bash
go run ./cmd/pushagent --restore cmd/pushagent/backup/<file>.json
```

The PATCH only touches `conversation_config.agent` (first message, language,
prompt and `built_in_tools.end_call`); voice, Twilio number and webhook stay
as they are. If the dashboard shows a draft, **Publish** the agent so calls
use the new version.

## Test it

Each real call costs credits (about $0.30–0.47 by phone); use test phrases,
never the real ones.

1. **Dashboard.** ElevenLabs > Agents > Mamá > **Test AI agent**. Fill the
   dynamic variables when it asks (or set test values under Agent >
   Dynamic variables): `nombre=Dani`, a test alert phrase and a test safe
   phrase.
2. **WebRTC fallback (browser).** Serve `spikes/call/webrtc.html`
   (`cd spikes/call && python3 -m http.server 8000`) and open
   `http://localhost:8000/webrtc.html?agent=<ELEVENLABS_AGENT_ID>`. That page
   does not send the variables yet, so before pressing the call button run in
   the browser console:

   ```js
   document.querySelector("elevenlabs-convai").setAttribute("dynamic-variables",
     JSON.stringify({nombre: "Dani", frase_alerta: "<test alert>", frase_segura: "<test safe>"}))
   ```

Make three calls and keep the transcripts (ElevenLabs > Agents > Call
history):

| Call | You say after the greeting | Mamá should |
| --- | --- | --- |
| Safe | the test safe phrase | say goodbye quickly and hang up |
| Alert | the test alert phrase | keep the tone, ask at most 5 everyday questions one at a time, say goodbye, hang up |
| None | something else ("Bien, aquí en el tráfico") | ask once more, then where and with whom, say goodbye, hang up |

Also try `frase_segura=""`: only the alert phrase should change her course.
