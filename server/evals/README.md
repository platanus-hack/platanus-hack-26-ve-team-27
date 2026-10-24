# Duress detector eval dialogs

Labeled Spanish (Venezuelan) transcripts of the fake "Mamá" call, used to
measure the duress detector (#19 rules, #22 LLM step + eval runner, #106 safe
phrase) and the call-context extraction (#106).
Data and committed results only: no code lives here.

## Running the eval

```bash
cd server && go run ./cmd/evals
```

The runner lives in `server/cmd/evals/`. It scores Rules, the LLM alone and
Chain (rules, then the LLM on unclear calls) and rewrites `results.md`, the
committed precision and recall. Dialogs without a `safe_phrase` (the #19/#22
set) are scored apart from those with one, so the first section stays
comparable with the numbers from before #106; the second also checks each
dialog's `expect`. The call-context section runs the extraction over
`ficha_dialogs.jsonl`. The LLM runs need `ANTHROPIC_API_KEY` (optional
`DURESS_LLM_MODEL`); without a key the report says the LLM was not measured.
To verify, run it and compare with the committed `results.md`.

## Files

- `duress_dialogs.jsonl`: one dialog per line, UTF-8 JSON.
- `ficha_dialogs.jsonl`: call-context dialogs, one per line (schema below).
- `results.md`: the last eval report, written by the runner.

## Schema

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | Stable id, `d001`, `d002`, ... |
| `duress` | bool | Label. `true` when the family must be alerted. |
| `near_miss` | bool | `true` for a no-duress dialog that uses words close to the phrase (same nouns, partial phrase, negation, phrase said by Mamá). Always `false` when `duress` is `true`. |
| `kind` | string | `exact` (phrase said as configured), `variant` (close variant: reordered, a word swapped, ASR slip, cut by noise), `silence` (user never answers in words), `normal` (ordinary call), `near_miss`. With a safe phrase also: `safe` (safe phrase or a close variant), `both` (both phrases), `neither` (talked, said neither, including vague answers such as "no sé, ma"), `safe_negated` ("no voy llegando"). |
| `duress_phrase` | string | The phrase the user configured at setup. Phrases vary across dialogs. |
| `safe_phrase` | string | Optional. The "todo bien" phrase (#106). Absent on the #19/#22 dialogs. |
| `expect` | string | Optional. The verdict the rules must give (`duress`, `no_duress`, `unclear`); set on every safe-phrase dialog, where "said neither" is `unclear` by design. |
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

### The two phrases (#106)

With a `safe_phrase`, these apply in order (the detector's rules and the
LLM's `systemPrompt` say the same):

1. The duress phrase or a close variant, not negated: duress. Saying both
   phrases is duress: the alert wins.
2. Silence the whole call: duress.
3. The safe phrase or a close variant, not negated: no duress. A "no"
   attached to it does not count ("no voy llegando" is not "voy llegando");
   a "no" that answers Mamá before it ("No... ya voy llegando") does.
4. Neither phrase, having talked: unclear (`expect: unclear`, `duress:
   false`). The person did not confirm they are fine, and the family is
   told so.

Only what the user (or a third party on their line) says counts, never Mamá.

## Call-context dialogs (`ficha_dialogs.jsonl`)

| Field | Type | Meaning |
| --- | --- | --- |
| `id` | string | `f001`, `f002`, ... |
| `duress_phrase`, `safe_phrase` | string | The configured phrases, sent as code phrases that are not facts. |
| `turns` | array | As above. |
| `known` | array | Which of `lugar`, `personas`, `vehiculo`, `rumbo` the call lets a reader know. The rest must come back `no se sabe`. |
| `notes` | string | Why. |

The runner counts, over those four fields, the ones the extraction filled
although the call did not say them (invented: the number that matters, it
must stay 0) and the ones it left `no se sabe` although the call said them
(missed). `situacion` and `confianza` are shown, not scored.
