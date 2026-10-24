#!/usr/bin/env bash
# Build the signed release APK.
#
# Signing credentials are resolved in this order (first match wins):
#   1. MAMA_KEYSTORE_PATH / MAMA_KEYSTORE_PASSWORD / MAMA_KEY_ALIAS / MAMA_KEY_PASSWORD env vars
#   2. keystore.properties at the repo root (gitignored)
#   3. ~/.mama-keys/keystore.properties (exported as the env vars above)
# With none of them the Gradle release build fails; it never falls back to debug signing.
#
# Output: app/build/outputs/apk/release/app-release.apk
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -z "${JAVA_HOME:-}" && -d "/Applications/Android Studio.app/Contents/jbr/Contents/Home" ]]; then
  export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
fi

if [[ ! -f local.properties && -z "${ANDROID_HOME:-}" && -z "${ANDROID_SDK_ROOT:-}" && -d "$HOME/Library/Android/sdk" ]]; then
  export ANDROID_HOME="$HOME/Library/Android/sdk"
fi

HOME_PROPS="$HOME/.mama-keys/keystore.properties"
if [[ -z "${MAMA_KEYSTORE_PATH:-}" && ! -f "$ROOT/keystore.properties" && -f "$HOME_PROPS" ]]; then
  echo "release: using signing config from $HOME_PROPS"
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      storeFile) export MAMA_KEYSTORE_PATH="$value" ;;
      storePassword) export MAMA_KEYSTORE_PASSWORD="$value" ;;
      keyAlias) export MAMA_KEY_ALIAS="$value" ;;
      keyPassword) export MAMA_KEY_PASSWORD="$value" ;;
    esac
  done < "$HOME_PROPS"
fi

./gradlew --no-daemon :app:assembleRelease

APK="$ROOT/app/build/outputs/apk/release/app-release.apk"
if [[ ! -f "$APK" ]]; then
  echo "release: expected $APK but it was not produced" >&2
  exit 1
fi

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"
APKSIGNER="$(ls -d "$SDK"/build-tools/*/apksigner 2>/dev/null | sort -V | tail -n 1 || true)"
if [[ -n "$APKSIGNER" ]]; then
  "$APKSIGNER" verify --print-certs "$APK" 2>&1 | grep -E 'Signer #1 certificate (DN|SHA-256)'
else
  echo "release: apksigner not found under $SDK/build-tools, skipping signature check"
fi

echo "release: signed APK at $APK"
