#!/usr/bin/env python3
"""Place one ElevenLabs + Twilio call and time it (#11 spike).

Standard library only. Reads keys from the environment (never prints them):
  ELEVENLABS_API_KEY, ELEVENLABS_AGENT_ID, ELEVENLABS_PHONE_NUMBER_ID,
  TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, optional ELEVENLABS_VOICE_ID.

    set -a; source /path/to/.env; set +a
    python3 spikes/call/place_call.py +584141234567

What it measures:
  request    POST outbound-call sent -> ElevenLabs answers (callSid known)
  ringing    POST sent -> Twilio reports the call "ringing"
  answer     POST sent -> Twilio reports "in-progress" (phone picked up)
  first word answer -> you press Enter when you hear the agent's first word

Twilio status is polled every POLL_S seconds, so ringing/answer are accurate
to about that much. "First word" is a human stopwatch: press Enter the moment
you hear the voice. With --via-server URL the call goes through the server's
POST /spike/call (needs CALL_SPIKE_TOKEN) instead of straight to ElevenLabs.
"""

import argparse
import base64
import json
import os
import sys
import threading
import time
import urllib.error
import urllib.request

ELEVENLABS = "https://api.elevenlabs.io"
TWILIO = "https://api.twilio.com/2010-04-01"
POLL_S = 0.25
FINAL = {"completed", "busy", "failed", "no-answer", "canceled"}


def env(name, required=True):
    v = os.environ.get(name, "").strip()
    if required and not v:
        sys.exit(f"missing env var {name} (load the .env first)")
    return v


def http_json(method, url, headers=None, body=None, timeout=30):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers=headers or {})
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, json.loads(resp.read() or b"null")
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")[:500]


def mask(number):
    return number[:4] + "…" + number[-2:] if len(number) > 6 else "…"


def place_direct(to, incident_id):
    body = {
        "agent_id": env("ELEVENLABS_AGENT_ID"),
        "agent_phone_number_id": env("ELEVENLABS_PHONE_NUMBER_ID"),
        "to_number": to,
        "conversation_initiation_client_data": {"dynamic_variables": {"incident_id": incident_id}},
    }
    voice = env("ELEVENLABS_VOICE_ID", required=False)
    if voice:
        body["conversation_initiation_client_data"]["conversation_config_override"] = {"tts": {"voice_id": voice}}
    status, out = http_json(
        "POST", ELEVENLABS + "/v1/convai/twilio/outbound-call",
        {"xi-api-key": env("ELEVENLABS_API_KEY")}, body)
    if status != 200 or not isinstance(out, dict) or not out.get("success"):
        sys.exit(f"ElevenLabs refused the call: {status} {out}")
    return out.get("conversation_id", ""), out.get("callSid", "")


def place_via_server(server, to, incident_id):
    status, out = http_json(
        "POST", server.rstrip("/") + "/spike/call",
        {"Authorization": "Bearer " + env("CALL_SPIKE_TOKEN")},
        {"to": to, "incident_id": incident_id})
    if status != 200 or not isinstance(out, dict):
        sys.exit(f"server refused the call: {status} {out}")
    return out.get("conversation_id", ""), out.get("call_sid", "")


def twilio_status(call_sid):
    sid = env("TWILIO_ACCOUNT_SID")
    auth = base64.b64encode(f"{sid}:{env('TWILIO_AUTH_TOKEN')}".encode()).decode()
    status, out = http_json(
        "GET", f"{TWILIO}/Accounts/{sid}/Calls/{call_sid}.json",
        {"Authorization": "Basic " + auth}, timeout=10)
    return out if status == 200 and isinstance(out, dict) else {}


def ms(a, b):
    return "" if a is None or b is None else str(round((b - a) * 1000))


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("to", help="E.164 number to call, e.g. +584141234567")
    p.add_argument("--via-server", metavar="URL", help="place the call through the server's POST /spike/call")
    p.add_argument("--max-wait", type=float, default=180, help="seconds to follow the call (default 180)")
    args = p.parse_args()
    incident_id = f"spike-{int(time.time())}"

    t0 = time.monotonic()
    if args.via_server:
        conv, call_sid = place_via_server(args.via_server, args.to, incident_id)
    else:
        conv, call_sid = place_direct(args.to, incident_id)
    t_req = time.monotonic()
    print(f"placed: conversation {conv} call {call_sid} to {mask(args.to)} in {ms(t0, t_req)} ms")
    if not call_sid:
        sys.exit("no callSid in the answer; cannot follow the call on Twilio")

    first_word = {}
    def wait_enter():
        sys.stdin.readline()
        first_word["t"] = time.monotonic()
    print("Answer the phone. Press Enter the moment you hear the agent's first word.")
    threading.Thread(target=wait_enter, daemon=True).start()

    seen, last = {}, None
    final = None
    while time.monotonic() - t0 < args.max_wait:
        call = twilio_status(call_sid)
        st = call.get("status")
        if st and st != last:
            seen.setdefault(st, time.monotonic())
            print(f"  +{ms(t0, seen[st])} ms  twilio status: {st}")
            last = st
        if st in FINAL:
            final = call
            break
        time.sleep(POLL_S)

    t_ring, t_answer = seen.get("ringing"), seen.get("in-progress")
    print()
    print(f"request -> accepted : {ms(t0, t_req)} ms")
    print(f"request -> ringing  : {ms(t0, t_ring) or 'not seen'} ms")
    print(f"request -> answered : {ms(t0, t_answer) or 'not seen'} ms")
    print(f"answer -> first word: {ms(t_answer, first_word.get('t')) or 'not recorded'} ms")
    if final:
        print(f"twilio final status : {final.get('status')} duration {final.get('duration')} s"
              f" price {final.get('price')} {final.get('price_unit')}")
    else:
        print("twilio: call still open or status unreadable after --max-wait")

    # Credits: ElevenLabs fills the conversation metadata a few seconds after
    # the call ends. Print every cost/charging field it has; do not guess.
    if conv and env("ELEVENLABS_API_KEY", required=False):
        time.sleep(5)
        status, out = http_json("GET", f"{ELEVENLABS}/v1/convai/conversations/{conv}",
                                {"xi-api-key": env("ELEVENLABS_API_KEY")})
        meta = out.get("metadata", {}) if isinstance(out, dict) else {}
        cost = {k: v for k, v in meta.items() if "cost" in k or "charging" in k}
        print(f"elevenlabs status   : {out.get('status') if isinstance(out, dict) else status}")
        print(f"elevenlabs cost     : {json.dumps(cost) if cost else 'not in metadata yet; check the dashboard'}")

    print()
    print("Row for spikes/call/RESULTS.md:")
    print(f"| {time.strftime('%H:%M')} | {mask(args.to)} | {final.get('status') if final else '?'} "
          f"| {ms(t0, t_req)} | {ms(t0, t_ring)} | {ms(t_answer, first_word.get('t'))} | | {conv} |")


if __name__ == "__main__":
    main()
