#!/usr/bin/env bash
# Builds and tests the whole repo: the Go server, then the Android app.
# Run from anywhere: scripts/build.sh. Exits non-zero on the first failure.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Gradle needs a JDK. On macOS fall back to the one bundled with Android Studio.
if [[ -z "${JAVA_HOME:-}" && "$(uname -s)" == "Darwin" ]]; then
  AS_JBR="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
  if [[ -d "$AS_JBR" ]]; then
    export JAVA_HOME="$AS_JBR"
  fi
fi

# Gradle needs the Android SDK: local.properties (sdk.dir) or ANDROID_HOME.
if [[ ! -f local.properties && -z "${ANDROID_HOME:-}" && -z "${ANDROID_SDK_ROOT:-}" ]]; then
  if [[ -d "$HOME/Library/Android/sdk" ]]; then
    export ANDROID_HOME="$HOME/Library/Android/sdk"
  elif [[ -d "$HOME/Android/Sdk" ]]; then
    export ANDROID_HOME="$HOME/Android/Sdk"
  fi
fi

step() { printf '\n==> %s\n' "$*"; }

# --- secret scan (ticket #4) ---------------------------------------------
# HOOK: ticket #4 adds the secret scan here, before anything is built.
# --------------------------------------------------------------------------

step "server: go vet"
(cd server && go vet ./...)

step "server: go test"
(cd server && go test ./...)

step "server: go build"
(cd server && go build ./...)

step "app: assembleDebug testDebugUnitTest"
./gradlew assembleDebug testDebugUnitTest

step "OK: server and app built and tested"
