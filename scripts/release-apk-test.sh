#!/usr/bin/env bash
# Self-test for the checks in scripts/release-apk.sh: the build-time config it refuses
# to publish without, and the secret checks on the APK's entries and contents. The fake
# keys are assembled at run time, so this file holds no literal key.
#
#   scripts/release-apk-test.sh
set -euo pipefail

# shellcheck source=release-apk.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release-apk.sh"
unset MAMA_KEYSTORE_PASSWORD MAMA_KEY_PASSWORD
WORK="$(mktemp -d "${TMPDIR:-/tmp}/release-apk-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

rep() { local s="" i; for ((i = 0; i < $2; i++)); do s="$s$1"; done; printf '%s' "$s"; }

fails=0
check() { # check <want: pass|fail> <label> <command...>
  local want="$1" label="$2" got=pass
  shift 2
  "$@" >/dev/null 2>&1 || got=fail
  if [[ "$got" == "$want" ]]; then
    printf 'ok    %s\n' "$label"
  else
    printf 'FAIL  %s (got %s, want %s)\n' "$label" "$got" "$want"
    fails=$((fails + 1))
  fi
}

check pass "server url: render https"            check_server_url "https://mama-server.onrender.com"
check pass "server url: trailing slash"          check_server_url "https://mama-server.onrender.com/"
check fail "server url: unset"                   check_server_url ""
check fail "server url: plain http"              check_server_url "http://mama-server.onrender.com"
check fail "server url: README placeholder"      check_server_url "https://<pending>.onrender.com"
check fail "server url: pending host"            check_server_url "https://pending.onrender.com"
check fail "server url: debug default"           check_server_url "https://127.0.0.1:8080"
check fail "server url: localhost"               check_server_url "https://localhost"
check fail "server url: with a path"             check_server_url "https://mama-server.onrender.com/devices"

check pass "twilio: E.164"                       check_twilio_number "+15550100000"
check fail "twilio: unset"                       check_twilio_number ""
check fail "twilio: no plus"                     check_twilio_number "15550100000"
check fail "twilio: placeholder"                 check_twilio_number "+1XXXXXXXXXX"
check fail "twilio: spaces"                      check_twilio_number "+1 555 010 0000"

entries() { printf '%s\n' "$@" | check_apk_entries; }
check pass "entries: a normal APK"               entries AndroidManifest.xml classes.dex META-INF/CERT.RSA res/raw/a.json
check fail "entries: jks"                        entries classes.dex assets/mama-release.jks
check fail "entries: keystore"                   entries release.keystore
check fail "entries: keystore.properties"        entries assets/keystore.properties
check fail "entries: local.properties"           entries local.properties
check fail "entries: .env"                       entries assets/.env
check fail "entries: .env.production"            entries .env.production
check fail "entries: pem"                        entries assets/key.pem

contents() { # contents <text> [literal...]: one file holding <text>
  rm -rf "$WORK/apk"; mkdir -p "$WORK/apk"; printf 'x%sx\n' "$1" >"$WORK/apk/classes.dex"
  shift; check_apk_contents "$WORK/apk" "$@"
}
check pass "contents: server url and number"     contents "https://mama-server.onrender.com +15550100000"
check fail "contents: private key block"         contents "-----BEGIN ""PRIVATE KEY-----"
check fail "contents: ElevenLabs key"            contents "sk_$(rep 0a 24)"
check fail "contents: Twilio account SID"        contents "AC$(rep a1 16)"
check fail "contents: AWS key"                   contents "AKIA$(rep AB 8)"
check fail "contents: GitHub token"              contents "ghp_$(rep aB3 12)"
check fail "contents: database URL"              contents "postgres""://mama:hunter2""@db.example/mama"
check fail "contents: signing password"          contents "padding s3cret-Pw padding" "s3cret-Pw"
check pass "contents: empty literal ignored"     contents "nothing here" ""
check fail "contents: unreadable dir is not clean" check_apk_contents "$WORK/missing"

# signing_passwords reads the same source Gradle uses: env, then ./keystore.properties, then ~/.mama-keys.
passwords_are() { # passwords_are <want> <dir> <home>
  local got
  got="$(cd "$2" && HOME="$3" signing_passwords | tr '\n' ' ')"
  [[ "$got" == "$1" ]]
}
mkdir -p "$WORK/repo" "$WORK/home/.mama-keys" "$WORK/empty"
printf 'storeFile=/k.jks\r\nstorePassword = home-store\r\nkeyAlias=a\r\nkeyPassword=home-key\r\n' >"$WORK/home/.mama-keys/keystore.properties"
check pass "passwords: ~/.mama-keys, CRLF"       passwords_are "home-store home-key " "$WORK/empty" "$WORK/home"
printf 'storePassword=repo-store\nkeyPassword=repo-key\n' >"$WORK/repo/keystore.properties"
check pass "passwords: keystore.properties wins" passwords_are "repo-store repo-key " "$WORK/repo" "$WORK/home"
check pass "passwords: none configured"          passwords_are "" "$WORK/empty" "$WORK/empty"
env_wins() { MAMA_KEYSTORE_PASSWORD=env-store MAMA_KEY_PASSWORD=env-key passwords_are "env-store env-key " "$WORK/repo" "$WORK/home"; }
check pass "passwords: env vars win"             env_wins

if [[ $fails -gt 0 ]]; then
  echo "release-apk self-test: $fails failed"
  exit 1
fi
echo "release-apk self-test: passed"
