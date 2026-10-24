#!/usr/bin/env bash
# Self-test for scripts/secret-scan.sh. Plants fake keys in a throwaway git repo
# and checks that each one is caught, and that placeholders, gitignored files
# and allow-marked lines are not. The fake keys are assembled at run time, so
# this file holds no literal key and the real scan stays clean.
#
#   scripts/secret-scan-test.sh
set -euo pipefail

SCAN="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/secret-scan.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/secret-scan-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

rep() { local s="" i; for ((i = 0; i < $2; i++)); do s="$s$1"; done; printf '%s' "$s"; }
HEX32="$(rep a1 16)"
HEX48="$(rep 9f 24)"
ALLOW="secret-scan""":allow

fails=0
newrepo() { rm -rf "$WORK/r"; mkdir -p "$WORK/r"; git -C "$WORK/r" init -q; }

expect() { # expect <exit-code> <label>
  local want="$1" label="$2" got=0
  "$SCAN" "$WORK/r" >"$WORK/out" 2>&1 || got=$?
  if [[ "$got" == "$want" ]]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s (exit %s, want %s)\n' "$label" "$got" "$want"; sed 's/^/      /' "$WORK/out"
    fails=$((fails + 1))
  fi
}

plant() { # plant <label> <file> <line>: the scan must fail on that one line
  newrepo; mkdir -p "$WORK/r/$(dirname "$2")"; printf '%s\n' "$3" >"$WORK/r/$2"
  expect 1 "catches $1"
}

clean() { # clean <label> <file> <line>: the scan must pass
  newrepo; mkdir -p "$WORK/r/$(dirname "$2")"; printf '%s\n' "$3" >"$WORK/r/$2"
  expect 0 "ignores $1"
}

plant "ElevenLabs key" app/Keys.kt "val k = \"sk_$HEX48\""
plant "Twilio Account SID" server/planted.go "const sid = \"AC$HEX32\""
plant "Twilio API key SID" server/planted.go "key := \"SK$HEX32\""
plant "Twilio auth token" server/planted.go "authToken := \"$HEX32\""
plant "Kapso key assignment" server/.envrc.sample "KAPSO_API_KEY=kp_$(rep Zq9 8)"
plant "OpenAI style key" notes.md "sk-proj-$(rep Ab3x 10)"
plant "Anthropic style key" notes.md "sk-ant-api03-$(rep Ab3x 10)"
plant "AWS access key id" deploy.sh "AKIA$(rep Q7 8)"
plant "private key block" key.txt "-----BEGIN RSA PRIVATE"" KEY-----"
plant "generic password" app/Config.kt "const val PASSWORD = \"$(rep hunter2 3)\""
plant "postgres URL with password" render.txt "postgres://mama:$(rep pW9 4)@dpg-x.virginia-postgres.render.com/mama"

clean "placeholders" README.md "KAPSO_API_KEY=your_kapso_api_key_here"
clean "env lookups" server/main.go "key := os.Getenv(\"TWILIO_AUTH_TOKEN\")"
clean "allow marker" fixture.go "sid := \"AC$HEX32\" // $ALLOW"

newrepo
printf '.env\n' >"$WORK/r/.gitignore"
printf 'TWILIO_ACCOUNT_SID=AC%s\n' "$HEX32" >"$WORK/r/.env"
expect 0 "ignores gitignored .env"

if ((fails > 0)); then
  printf '\nsecret-scan-test: %d failure(s)\n' "$fails" >&2
  exit 1
fi
printf '\nsecret-scan-test: all passed\n'
