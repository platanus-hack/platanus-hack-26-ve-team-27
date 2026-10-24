#!/usr/bin/env bash
# Build the signed release APK.
#
# Signing credentials are resolved in this order (first match wins):
#   1. MAMA_KEYSTORE_PATH / MAMA_KEYSTORE_PASSWORD / MAMA_KEY_ALIAS / MAMA_KEY_PASSWORD env vars (all four)
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
    [[ "$line" != *=* ]] && continue
    key="${line%%=*}"
    value="${line#*=}"
    key="$(printf '%s' "$key" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    value="$(printf '%s' "$value" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    case "$key" in
      storeFile) export MAMA_KEYSTORE_PATH="$value" ;;
      storePassword) export MAMA_KEYSTORE_PASSWORD="$value" ;;
      keyAlias) export MAMA_KEY_ALIAS="$value" ;;
      keyPassword) export MAMA_KEY_PASSWORD="$value" ;;
    esac
  done < "$HOME_PROPS"
  # Resolve "~/..." and paths relative to ~/.mama-keys.
  case "${MAMA_KEYSTORE_PATH:-}" in
    "~/"*) export MAMA_KEYSTORE_PATH="$HOME/${MAMA_KEYSTORE_PATH#\~/}" ;;
    /*|"") ;;
    *) export MAMA_KEYSTORE_PATH="$(dirname "$HOME_PROPS")/$MAMA_KEYSTORE_PATH" ;;
  esac
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
  # apksigner runs plain `java`, so put the JDK used for Gradle first on PATH.
  if [[ -n "${JAVA_HOME:-}" ]]; then export PATH="$JAVA_HOME/bin:$PATH"; fi
  if ! CERTS="$("$APKSIGNER" verify --print-certs "$APK" 2>&1)"; then
    echo "$CERTS" >&2
    echo "release: apksigner verify failed for $APK" >&2
    exit 1
  fi
  printf '%s\n' "$CERTS" | grep -E 'Signer #1 certificate (DN|SHA-256)' || true
else
  echo "release: apksigner not found under $SDK/build-tools, skipping signature check"
fi

echo "release: signed APK at $APK"
