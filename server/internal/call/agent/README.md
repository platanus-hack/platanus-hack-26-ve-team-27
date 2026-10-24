# Mamá agent (ElevenLabs)

The prompt and first message of the "Mamá" agent live here, not only in the
ElevenLabs dashboard:

| File | What |
| --- | --- |
| `prompt.md` | System prompt (`conversation_config.agent.prompt.prompt`) |
| `first_message.txt` | First message (`conversation_config.agent.first_message`) |
| `tts.json` | TTS model and voice settings (`conversation_config.tts`) |
| `agent.go` | Embeds the three files and builds the PATCH body: prompt, first message, language `es`, system tool `end_call`, TTS settings |

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

The example lines are written the way they are said in Venezuela ("mi amor",
"¡Bendición!", "¿Y por dónde vas ya?"), short and fully punctuated: the TTS
reads `¿?`, `¡!`, commas and `...` as intonation and pauses, so punctuation
changes how Mamá sounds.

## Voice (`tts.json`)

So that Mamá sounds as close as possible to the cloned voice, the TTS
settings are versioned here and `pushagent` sends them in
`conversation_config.tts`. It never sends `voice_id`: the agent keeps its own
voice, and the per-device voice comes from the per-call override (#104).

| Setting | Value | What it does |
| --- | --- | --- |
| `model_id` | `eleven_flash_v2_5` | TTS model. Agents speak Spanish with the multilingual models: Flash v2.5 (lowest latency, chosen), Turbo v2.5, Multilingual v2, v3 conversational, v4 and v4 Turbo. `eleven_turbo_v2` and `eleven_flash_v2` are English only and are refused. |
| `stability` | 0.45 | Lower gives more expression and less consistency between turns; higher sounds flatter but steadier. |
| `similarity_boost` | 0.85 | Higher brings the voice closer to the original recording, but also copies its defects (noise, room echo, mouth sounds). |
| `speed` | 1.0 | Speaking rate, 0.7 to 1.2. |

`style` and `use_speaker_boost` exist for plain text-to-speech, but the agent
API has no field for them (checked against
[agents/update](https://elevenlabs.io/docs/api-reference/agents/update),
`conversation_config.tts`), so they are not in `tts.json` and the agent
cannot set them. For `style` the wanted value was 0 anyway: `style` > 0
exaggerates the voice and adds latency. Speaker boost cannot be turned on
for an agent; if the voice sounds thin, raise `similarity_boost` instead. `LoadTTS` refuses unknown keys, an English-only
model and out-of-range values, so a setting the API would ignore fails the
tests instead.

## Upload

Edit `prompt.md`, `first_message.txt` or `tts.json`, then:

```bash
set -a; source <main repo>/.env; set +a   # ELEVENLABS_API_KEY, ELEVENLABS_AGENT_ID
cd server
go run ./cmd/pushagent --dry-run          # print the body, send nothing
go run ./cmd/pushagent                    # back up, then PATCH the agent
```

Before writing, the command saves the agent's current configuration (the
`GET /v1/convai/agents/{agent_id}` answer) to
`server/cmd/pushagent/backup/<agent_id>-<UTC time>.json` (gitignored). To go
back, send that backup's agent and tts sections again (add `--dry-run` to
see them first):

```bash
go run ./cmd/pushagent --restore cmd/pushagent/backup/<file>.json
```

The PATCH only touches `conversation_config.agent` (first message, language,
prompt and `built_in_tools.end_call`) and `conversation_config.tts` (model,
stability, similarity boost, speed). The voice, Twilio number and webhook
stay as they are. If the dashboard shows a draft, **Publish** the agent so calls
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
