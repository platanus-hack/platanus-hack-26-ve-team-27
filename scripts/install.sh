#!/usr/bin/env bash
# Install the release APK over adb and launch the app.
#
# Usage:
#   scripts/install.sh                 # every connected device
#   scripts/install.sh <serial> ...    # only the given devices (see `adb devices`)
#   scripts/install.sh --reinstall ... # uninstall first (needed when a differently signed build is installed)
#   APK=path/to/other.apk scripts/install.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE="com.team27.mama"
APK="${APK:-$ROOT/app/build/outputs/apk/release/app-release.apk}"

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"
if command -v adb >/dev/null 2>&1; then
  ADB="$(command -v adb)"
elif [[ -x "$SDK/platform-tools/adb" ]]; then
  ADB="$SDK/platform-tools/adb"
else
  echo "install: adb not found (install platform-tools in Android Studio's SDK Manager)" >&2
  exit 1
fi

REINSTALL=0
SERIALS=()
for arg in "$@"; do
  case "$arg" in
    --reinstall) REINSTALL=1 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) SERIALS+=("$arg") ;;
  esac
done

if [[ ! -f "$APK" ]]; then
  echo "install: $APK not found, run scripts/release.sh first" >&2
  exit 1
fi

if [[ ${#SERIALS[@]} -eq 0 ]]; then
  while read -r serial state; do
    [[ -z "$serial" ]] && continue
    if [[ "$state" == "device" ]]; then
      SERIALS+=("$serial")
    else
      echo "install: skipping $serial ($state). Unlock it and accept the USB debugging prompt." >&2
    fi
  done < <("$ADB" devices | tail -n +2)
fi

if [[ ${#SERIALS[@]} -eq 0 ]]; then
  echo "install: no authorized device found. Connect a phone over USB, enable USB debugging and accept the RSA prompt." >&2
  "$ADB" devices -l >&2
  exit 1
fi

failed=0
for serial in "${SERIALS[@]}"; do
  model="$("$ADB" -s "$serial" shell getprop ro.product.model 2>/dev/null | tr -d '\r' || true)"
  echo "install: $serial (${model:-unknown model})"
  if [[ $REINSTALL -eq 1 ]]; then
    "$ADB" -s "$serial" uninstall "$PACKAGE" >/dev/null 2>&1 || true
  fi
  if ! "$ADB" -s "$serial" install -r "$APK"; then
    echo "install: failed on $serial. INSTALL_FAILED_UPDATE_INCOMPATIBLE -> rerun with --reinstall; INSTALL_FAILED_USER_RESTRICTED (Xiaomi) -> enable 'Install via USB' and accept the prompt on the phone." >&2
    failed=1
    continue
  fi
  if "$ADB" -s "$serial" shell monkey -p "$PACKAGE" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1; then
    echo "install: launched $PACKAGE on $serial"
  else
    echo "install: installed on $serial but could not launch it, open Aló Mamá from the launcher" >&2
  fi
done

exit $failed
