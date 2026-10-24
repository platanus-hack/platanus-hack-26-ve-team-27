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
alert phrase counts.

`pushagent` also sets their defaults
(`conversation_config.agent.dynamic_variables.dynamic_variable_placeholders`,
`Placeholders` in `agent.go`): `nombre="mi vida"` and both phrases empty. A
caller that sends no variables (calls placed before #104, the WebRTC page)
gets "¡Hola, mi vida!" and the "neither phrase" path, so pushing the prompt
does not break those calls. The default phrases stay empty so no ordinary
answer can match one by chance.

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
go run ./cmd/pushagent --agent agent_...  # back up, then PATCH that agent
go run ./cmd/pushagent                    # same, for $ELEVENLABS_AGENT_ID
```

Try changes on the test agent `mama-test-105`
(`agent_6201m5qqkx93f1vt17p8b21qtrf7`) first. `ELEVENLABS_AGENT_ID` is the
shared agent the team demo uses: push to it only when the team agrees.

Before writing, the command saves the agent's current configuration (the
`GET /v1/convai/agents/{agent_id}` answer) to
`server/cmd/pushagent/backup/<agent_id>-<UTC time>.json` (gitignored). To go
back, send that backup's agent and tts sections again (add `--dry-run` to
see them first):

```bash
go run ./cmd/pushagent --restore cmd/pushagent/backup/<file>.json
```

The PATCH only touches `conversation_config.agent` (first message, language,
variable defaults, prompt and `built_in_tools.end_call`) and `conversation_config.tts` (model,
stability, similarity boost, speed). The voice, Twilio number and webhook
stay as they are. If the dashboard shows a draft, **Publish** the agent so calls
use the new version.

## Test it

Each real call costs credits (about $0.30–0.47 by phone); use test phrases,
never the real ones.

1. **Text simulation (cheapest, no audio).** ElevenLabs plays the user with
   an LLM against the agent:
   `POST /v1/convai/agents/{agent_id}/simulate-conversation` with
   `simulation_specification.simulated_user_config.prompt.prompt` (who the
   user is and what to say), `simulation_specification.dynamic_variables`
   (`nombre`, test phrases) and `new_turns_limit`. The answer has
   `simulated_conversation` (the transcript, including the `end_call` tool
   call). ElevenLabs marks this endpoint deprecated, to be removed on
   31 Oct 2026; after that, use the dashboard's tests.
2. **Dashboard.** ElevenLabs > Agents > Mamá > **Test AI agent**. Fill the
   dynamic variables when it asks (or set test values under Agent >
   Dynamic variables): `nombre=Dani`, a test alert phrase and a test safe
   phrase.
3. **WebRTC fallback (browser).** Serve `spikes/call/webrtc.html`
   (`cd spikes/call && python3 -m http.server 8000`) and open
   `http://localhost:8000/webrtc.html?agent=<agent id>`. That page does not
   send the variables (you get the defaults), so to test the phrases run this
   in the browser console before pressing the call button:

   ```js
   document.querySelector("elevenlabs-convai").setAttribute("dynamic-variables",
     JSON.stringify({nombre: "Dani", frase_alerta: "<test alert>", frase_segura: "<test safe>"}))
   ```

Run the three cases and keep the transcripts (simulation answer, or
ElevenLabs > Agents > Call history):

| Case | User says after the greeting | Mamá should |
| --- | --- | --- |
| Safe | the test safe phrase | say goodbye quickly and hang up |
| Alert | the test alert phrase | keep the tone, ask at most 5 everyday questions one at a time, say goodbye, hang up |
| None | something else ("Bien, aquí en el tráfico") | ask once more, then only where and with whom, say goodbye, hang up |

Also try `frase_segura=""`: only the alert phrase should change her course.
