# Aló Mamá — Project Summary

*Platanus Hack 26 Caracas · Idea locked Sat Oct 24, 2026, 03:05 VET*

## What it is

Aló Mamá is an Android app that lets someone ask for help in secret when they're in danger, for example during a kidnapping or a robbery. The person presses volume **up-down-up**, or taps a home screen widget, and within 15 seconds they get a call from "Mamá". "Mamá" is an AI voice agent. If the person says the duress phrase during the call (*"dile a la tía Carmen que llego tarde"*), or stays silent, the family gets a WhatsApp right away with the location and the verdict. To anyone watching, it looks like an ordinary call with mom.

## Why we chose it

- **Mentor score:** Mostro rates it about 4.5/5 (Originality 3.5, A 5, E 4, I 5, T 5). It's strongest on the criteria the judges weigh most (T25, I20).
- **Real pain:** personal safety is an everyday concern in Venezuela.
- **Strong demo:** two physical phones, a live call, and a WhatsApp landing in front of the judges.
- **Sponsor fit:** it naturally uses ElevenLabs, Twilio and Kapso.
- **Feasible:** it fits the 35-hour window with a 5-person team if login, web and iOS stay out.

## How it works

1. **Trigger.** The volume pattern up-down-up, detected through the accessibility service. Backups are the home screen widget, plus a quick settings tile or an in-app button.
2. **Capture.** GPS, speed and audio (where Android allows it) are sent to the server.
3. **Three branches, chosen by speed:**
   - Moving fast (for example, in a car): silent tracker mode.
   - Still: immediate alert to the family.
   - Otherwise: "Mamá" calls.
4. **The call.** ElevenLabs Agents plus a Twilio outbound call. The voice is fixed: Mariana's own, cloned with her consent. A webhook returns the transcript and the verdict.
5. **Duress detector.** Rules run first, then the LLM. Evals (precision and recall over 50+ dialogs) live in the repo.
6. **Notifier.** A Go interface. The main implementation sends a single Kapso WhatsApp message (inside the 24-hour window) with the location and the verdict. A Twilio SMS fallback is built only if Kapso fails. There is **no live page and no web**.

## Stack

- **App:** native Kotlin (Jetpack Compose + Material3), installed with adb, no Play Store.
- **Server:** Go + Postgres on Render's paid tier, so nothing sleeps. Render deploys every commit to main, and `/version` returns the commit.
- **No auth:** the app generates a device ID at install.
- **First-run setup:** only asks for Android permissions, in this order: notifications, location (while in use, then always), mic, contacts, SMS, battery exemption, and accessibility last. It also stores the family numbers, the duress phrase, and the Twilio number saved as the contact "Mamá".
- **Repo:** one public repo in the platanus-hack org, with `app/` and `server/`.

## Demo and pitch

- **Format:** a 3-minute pitch with about 90 s of live demo, then 2 minutes of Q&A.
- **Customer:** families.
- **Presenter:** Mariana, with DanCas03 as backup.
- **The wow moment:** Mariana does the volume pattern, "Mamá" calls in under 15 s, she says the duress phrase, and the WhatsApp arrives on the family phone (our own) while a latency counter runs on screen.
- **Key line:** *"La única voz que puedo clonar esta noche con consentimiento es la mía. En producción, a ti te llama tu mamá de verdad."* ("The only voice I can clone tonight with consent is my own. In production, your real mom calls you.")
- **Competitors:** not named in the pitch; saved for Q&A.
- **Demo phone:** a Xiaomi (HyperOS), with a backup Android. scrcpy can mirror it.

## Extras (behind flags)

- **extra:1, offline location:** a local queue, a compact SMS to Twilio after about 20 s without data, and a WorkManager resend.
- **extra:2 (flex), "it turned off or disconnected":** detects airplane mode or shutdown, with server-side heartbeat escalation.
- **Cut order:** extra:2 goes first, then extra:1.

**Out of scope:** login, per-user voice cloning, blocking shutdown, Google SOS, trip monitor, fleet panel, iOS, Play Store, web, smartwatch.

## Team

| Group | Person | Owns |
|---|---|---|
| Q1 (core) | P1 Samuel | Wave 0b, the contract, the call engine, the 3 branches, the notifier, the final merge |
| Q1 | P2 Daniel Ross | The Kotlin app and the phone-server-call join |
| Q1 | P5 Mariana | Screens, eval dialogs, pitch, presenter |
| Q2 | P4 Daniel Castellanos (DanCas03) | APK build and sign template, the locked-phone trigger spike on the Xiaomi, both extras |
| Q2 | P3 Sebas | Duress detector with evals, OS research note, live tester |

## Schedule (VET)

| When | Milestone |
|---|---|
| Sat 05:05 | Spikes due: the trigger on the Xiaomi and the +58 call latency |
| Sat 07:05 | Contract freeze (and the Render URL) |
| Sat 09:05 | Join starts |
| Sat 11:05 | Walking skeleton |
| Sat 13:05 | Cut trigger |
| Sat 17:05 | Walkthrough |
| Sat 18:00 | Flex decision |
| Sun 02:00 | Rehearsal |
| Sun 03:00 | Feature freeze |
| Sun 05:00 | Backup video locked |
| Sun 06:00 | Submission |
| Sun 07:00 | Deploy cutoff |
| Sun 08:30 | End of coding window |

Tick checks in every 3 hours and posts to #tick.

## Board

- Index: https://github.com/platanus-hack/platanus-hack-26-ve-team-27/issues/7
- Wave 0a: #1 scaffold, #2 APK build and adb install, #3 Render deploy with /version and /health, #4 keys and limits, #5 Material3 theme, #8 visual identity and 2 reference screens. #6 (submission checklist) was closed as not needed.
- Wave 0b and Waves 1/2 are being published after the lock.

## Open items

- Pick the number for the family WhatsApp phone.
- Xiaomi: set up the Mi account and turn on "Install via USB".
- Ask the organizers what "made with Opus 5.5" means.
