# Duress detector eval dialogs

Labeled Spanish (Venezuelan) transcripts of the fake "Mamá" call, used to
measure the duress detector (#19 rules, #22 LLM step + eval runner).
Data and committed results only: no code lives here.

## Running the eval

```bash
cd server && go run ./cmd/evals
```

The runner lives in `server/cmd/evals/`. It scores Rules, the LLM alone and
Chain (rules, then the LLM on unclear calls) and rewrites `results.md`, the
committed precision and recall. The LLM runs need `ANTHROPIC_API_KEY`
(optional `DURESS_LLM_MODEL`); without a key the report says the LLM was not
measured. To verify, run it and compare with the committed `results.md`.

## Files

- `duress_dialogs.jsonl`: one dialog per line, UTF-8 JSON.
- `results.md`: the last eval report, written by the runner.

## Schema

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | Stable id, `d001`, `d002`, ... |
| `duress` | bool | Label. `true` when the family must be alerted. |
| `near_miss` | bool | `true` for a no-duress dialog that uses words close to the phrase (same nouns, partial phrase, negation, phrase said by Mamá). Always `false` when `duress` is `true`. |
| `kind` | string | `exact` (phrase said as configured), `variant` (close variant: reordered, a word swapped, ASR slip, cut by noise), `silence` (user never answers in words), `normal` (ordinary call), `near_miss`. |
| `duress_phrase` | string | The phrase the user configured at setup. Phrases vary across dialogs. |
| `turns` | array | `{"speaker": "mama" \| "user" \| "other", "text": "..."}` in call order. |
| `notes` | string | Why the label is what it is, for reviewers. |

### Turns

- `mama` is the AI caller, `user` is the person who triggered the call.
- `other` is a third party heard on the line (the robber, a taxi driver, a
  relative). In a real ElevenLabs transcript that audio lands in the user
  channel, so an eval runner that only has two roles should merge `other`
  into `user`. The detector must not alert just because `other` speaks.
- Noise and ASR artifacts are written inline: `[ruido]`, `[silencio]`,
  `[inaudible]`, `[se corta]`, `[risas]`, `[pito de carro]`,
  `[ruido de calle]`, `[respiración]`, free-text tags such as
  `[inaudible, voz masculina de fondo]`, and words cut with `-`.

### Labels

- A dialog is `duress: true` when the user says the configured phrase or a
  close variant of it (same meaning, same key words), or stays silent the
  whole call (`kind: silence`; `docs/MAMA-summary.md` says the family is
  alerted if the user "stays silent").
- Near misses are `duress: false`: the key words appear in another meaning,
  only half the phrase is said, the user negates it, or Mamá says it and the
  user does not repeat it. Some near misses contain most of the phrase on
  purpose (for example `d045`, the full phrase with "no" inserted); they
  test negation handling and are not labeling errors.
