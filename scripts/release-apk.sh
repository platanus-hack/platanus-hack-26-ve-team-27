#!/usr/bin/env bash
# Build the signed release APK from main's HEAD and publish it as a public GitHub Release (#37).
#
# Usage:
#   MAMA_SERVER_URL=https://<render-app>.onrender.com MAMA_TWILIO_NUMBER=+1XXXXXXXXXX scripts/release-apk.sh
#   ... scripts/release-apk.sh --dry-run   # build and check only; never publishes, works off main too
#
# It refuses to publish unless: both values are set and real (https, E.164), the tree is clean and
# HEAD is origin/main, the APK is signed with the team certificate, BuildConfig carries both values,
# and the APK holds no keystore, env file or key-shaped string. The Release is tagged apk-<short sha>,
# holds only app-release.apk and is marked latest, so this link always serves the newest one:
#   https://github.com/platanus-hack/platanus-hack-26-ve-team-27/releases/latest/download/app-release.apk
# Signing credentials are read as in scripts/release.sh (env vars, keystore.properties, ~/.mama-keys).
set -euo pipefail

REPO="platanus-hack/platanus-hack-26-ve-team-27"
ASSET="app-release.apk"
STABLE_URL="https://github.com/$REPO/releases/latest/download/$ASSET"
CERT_DN="CN=MAMA Team 27, O=Platanus Hack 26, C=VE"

die() { echo "release-apk: $*" >&2; exit 1; }
log() { echo "release-apk: $*"; }

# check_server_url <url>: an https origin of a real deploy, not a placeholder or a local address.
check_server_url() {
  local url="$1"
  if [[ -z "$url" ]]; then echo "MAMA_SERVER_URL is not set"; return 1; fi
  if [[ ! "$url" =~ ^https://[A-Za-z0-9.-]+\.[A-Za-z]{2,}(:[0-9]+)?/?$ ]]; then
    echo "MAMA_SERVER_URL must be https://<host> with no path (release builds forbid plain HTTP), got '$url'"; return 1
  fi
  case "$url" in
    *pending*|*example.*|*localhost*|*127.0.0.1*|*10.0.2.2*)
      echo "MAMA_SERVER_URL looks like a placeholder or a local address: '$url'"; return 1 ;;
  esac
}

# check_twilio_number <number>: E.164, the form the app saves as the "Mamá" contact.
check_twilio_number() {
  local number="$1"
  if [[ -z "$number" ]]; then echo "MAMA_TWILIO_NUMBER (or TWILIO_PHONE_NUMBER) is not set"; return 1; fi
  if [[ ! "$number" =~ ^\+[1-9][0-9]{7,14}$ ]]; then
    echo "MAMA_TWILIO_NUMBER must be E.164 (+ and 8 to 15 digits), got '$number'"; return 1
  fi
}

# check_apk_entries: reads APK entry names on stdin, prints and fails on signing material or env files.
check_apk_entries() {
  local bad
  bad="$(grep -E -i '(^|/)([^/]*\.(jks|keystore|p12|pfx|pem)|keystore\.properties|local\.properties|\.env(\.[^/]*)?)$' || true)"
  if [[ -n "$bad" ]]; then printf '%s\n' "$bad"; return 1; fi
}

# check_apk_contents <dir> [literal...]: fails on key-shaped strings, or any given literal
# (the signing passwords), in the unpacked APK. Prints the offending file names only.
check_apk_contents() {
  local dir="$1" hits found rc literal
  shift
  # grep exits 1 on no match; 2 is an error, and an errored scan must not read as clean.
  rc=0
  hits="$(LC_ALL=C grep -r -a -l -E \
    -e '-----BEGIN [A-Z ]*PRIVATE KEY-----' \
    -e 'sk_[0-9a-f]{40,}' \
    -e 'sk-(ant|proj)-[A-Za-z0-9_-]{20,}' \
    -e '(AC|SK)[0-9a-f]{32}' \
    -e 'AKIA[0-9A-Z]{16}' \
    -e 'gh[pousr]_[A-Za-z0-9]{36}' \
    -e 'postgres(ql)?://[^:/@[:space:]]+:[^@[:space:]]+@' \
    "$dir")" || rc=$?
  if [[ $rc -gt 1 ]]; then echo "grep failed scanning $dir"; return 1; fi
  for literal in "$@"; do
    [[ -z "$literal" ]] && continue
    rc=0
    found="$(LC_ALL=C grep -r -a -l -F -e "$literal" "$dir")" || rc=$?
    if [[ $rc -gt 1 ]]; then echo "grep failed scanning $dir"; return 1; fi
    hits+=$'\n'"$found"
  done
  hits="$(printf '%s\n' "$hits" | sed '/^$/d' | sort -u)"
  if [[ -n "$hits" ]]; then printf '%s\n' "$hits"; return 1; fi
}

sha256() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }

# signing_passwords: prints the store and key passwords from the source Gradle will use
# (env vars, then keystore.properties, then ~/.mama-keys), so the APK can be checked for them.
signing_passwords() {
  local file=""
  if [[ -n "${MAMA_KEYSTORE_PASSWORD:-}${MAMA_KEY_PASSWORD:-}" ]]; then
    printf '%s\n' "${MAMA_KEYSTORE_PASSWORD:-}" "${MAMA_KEY_PASSWORD:-}"; return
  fi
  if [[ -f keystore.properties ]]; then file=keystore.properties
  elif [[ -f "$HOME/.mama-keys/keystore.properties" ]]; then file="$HOME/.mama-keys/keystore.properties"
  fi
  [[ -n "$file" ]] || return 0
  tr -d '\r' <"$file" | sed -n -E 's/^[[:space:]]*(storePassword|keyPassword)[[:space:]]*=[[:space:]]*//p' | sed -E 's/[[:space:]]+$//'
}

WORK=""
cleanup() { if [[ -n "$WORK" ]]; then rm -rf "$WORK"; fi; }

main() {
  local dry_run=0 arg
  for arg in "$@"; do
    case "$arg" in
      --dry-run) dry_run=1 ;;
      -h|--help) sed -n '2,14p' "$0"; exit 0 ;;
      *) die "unknown argument '$arg' (see --help)" ;;
    esac
  done

  local root
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  cd "$root"

  # JDK and SDK: env first, then Android Studio's defaults on macOS and Windows.
  local jbr sdk
  if [[ -z "${JAVA_HOME:-}" ]]; then
    for jbr in "/Applications/Android Studio.app/Contents/jbr/Contents/Home" "/c/Program Files/Android/Android Studio/jbr"; do
      if [[ -d "$jbr" ]]; then export JAVA_HOME="$jbr"; break; fi
    done
  fi
  if [[ ! -f local.properties && -z "${ANDROID_HOME:-}" && -z "${ANDROID_SDK_ROOT:-}" ]]; then
    for sdk in "$HOME/Library/Android/sdk" "$HOME/Android/Sdk" "${LOCALAPPDATA:-/nonexistent}/Android/Sdk"; do
      if [[ -d "$sdk" ]]; then export ANDROID_HOME="$sdk"; break; fi
    done
  fi
  sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
  if [[ -z "$sdk" && -f local.properties ]]; then
    sdk="$(sed -n 's/^sdk\.dir=//p' local.properties | tr -d '\r' | sed 's/\\:/:/g; s/\\\\/\//g')"
  fi
  if [[ -n "${JAVA_HOME:-}" ]]; then export PATH="$JAVA_HOME/bin:$PATH"; fi

  # Build-time config (#15). Never guessed: both must be given.
  local server_url="${MAMA_SERVER_URL:-}" twilio="${MAMA_TWILIO_NUMBER:-${TWILIO_PHONE_NUMBER:-}}" why
  why="$(check_server_url "$server_url")" || die "$why"
  why="$(check_twilio_number "$twilio")" || die "$why"
  server_url="${server_url%/}"

  # Source: a clean checkout of origin/main's HEAD.
  git fetch -q origin main || die "git fetch origin main failed"
  local sha main_sha
  sha="$(git rev-parse HEAD)"
  main_sha="$(git rev-parse origin/main)"
  if [[ -n "$(git status --porcelain)" ]]; then
    [[ $dry_run -eq 1 ]] || die "the working tree is not clean; commit or remove changes first"
    log "warning: the working tree is not clean (dry run)"
  fi
  if [[ "$sha" != "$main_sha" ]]; then
    [[ $dry_run -eq 1 ]] || die "HEAD ($sha) is not origin/main ($main_sha); check out main and pull"
    log "warning: HEAD $sha is not origin/main $main_sha (dry run)"
  fi
  command -v unzip >/dev/null 2>&1 || die "unzip not found"
  if [[ $dry_run -eq 0 ]]; then
    command -v gh >/dev/null 2>&1 || die "gh (GitHub CLI) not found"
    gh auth status >/dev/null 2>&1 || die "gh is not logged in; run gh auth login"
    [[ "$(gh repo view "$REPO" --json visibility --jq .visibility)" == "PUBLIC" ]] ||
      die "$REPO is not public, so its Release link would not download"
  fi

  local apksigner=""
  if [[ -n "$sdk" ]]; then
    apksigner="$(ls -d "$sdk"/build-tools/*/apksigner "$sdk"/build-tools/*/apksigner.bat 2>/dev/null | sort -V | tail -n 1 || true)"
  fi
  [[ -n "$apksigner" ]] || die "apksigner not found under ${sdk:-<no SDK>}/build-tools; install build-tools in Android Studio's SDK Manager"

  # Build. -P beats ~/.gradle/gradle.properties and env vars, so the APK gets exactly these values.
  local apk="$root/app/build/outputs/apk/release/app-release.apk"
  rm -rf "$root/app/build/outputs/apk/release"
  log "building $sha with SERVER_URL=$server_url TWILIO_NUMBER=$twilio"
  scripts/release.sh "-Pmama.serverUrl=$server_url" "-Pmama.twilioNumber=$twilio"
  [[ -f "$apk" ]] || die "expected $apk but it was not produced"

  # Signed with the team certificate, never the debug one.
  local certs
  certs="$("$apksigner" verify --print-certs "$apk" 2>&1)" || { printf '%s\n' "$certs" >&2; die "apksigner verify failed"; }
  if ! printf '%s\n' "$certs" | grep -q -F "Signer #1 certificate DN: $CERT_DN"; then
    printf '%s\n' "$certs" | grep -E 'Signer #1 certificate DN' >&2 || true
    [[ $dry_run -eq 1 ]] || die "the APK is not signed with the team certificate ($CERT_DN)"
    log "warning: not signed with the team certificate (dry run)"
  fi

  WORK="$(mktemp -d "${TMPDIR:-/tmp}/release-apk.XXXXXX")"
  trap cleanup EXIT
  local work="$WORK"
  unzip -q -o "$apk" -d "$work/apk"

  # BuildConfig made it into the code.
  if ! grep -a -q -F -e "$server_url" "$work"/apk/classes*.dex; then die "SERVER_URL $server_url is not in the APK's code"; fi
  if ! grep -a -q -F -e "$twilio" "$work"/apk/classes*.dex; then die "TWILIO_NUMBER $twilio is not in the APK's code"; fi

  # Nothing secret inside.
  local bad passwords=()
  bad="$(unzip -Z1 "$apk" | check_apk_entries)" || die "the APK contains signing material or env files:"$'\n'"$bad"
  # A read loop, not mapfile: macOS ships bash 3.2, which has no mapfile.
  local line
  while IFS= read -r line; do passwords+=("$line"); done < <(signing_passwords)
  bad="$(check_apk_contents "$work/apk" ${passwords[@]+"${passwords[@]}"})" ||
    die "the APK contains a key-shaped string or a signing password in:"$'\n'"$bad"

  local sum
  sum="$(sha256 "$apk")"
  log "checked $apk (sha256 $sum)"

  if [[ $dry_run -eq 1 ]]; then
    log "dry run: not publishing"
    return 0
  fi

  # Publish: one Release per commit, holding only the APK, marked latest.
  local tag="apk-${sha:0:7}" notes
  notes="MAMÁ signed release APK built from $sha (main).

- Server: $server_url
- sha256: \`$sum\`

Stable download link: $STABLE_URL"
  local assets
  if gh release view "$tag" --repo "$REPO" >/dev/null 2>&1; then
    # Refuse before touching it if anything else was attached, so a stray file never goes latest.
    assets="$(gh release view "$tag" --repo "$REPO" --json assets --jq '.assets[].name' | grep -v -x -F "$ASSET" || true)"
    [[ -z "$assets" ]] || die "Release $tag already holds files other than $ASSET; remove them first: $assets"
    log "updating Release $tag"
    gh release upload "$tag" "$apk" --repo "$REPO" --clobber
    gh release edit "$tag" --repo "$REPO" --draft=false --prerelease=false --latest --notes "$notes"
  else
    log "creating Release $tag"
    gh release create "$tag" "$apk" --repo "$REPO" --target "$sha" --title "MAMÁ APK ${sha:0:7}" --notes "$notes" --latest
  fi

  assets="$(gh release view "$tag" --repo "$REPO" --json assets --jq '.assets[].name')"
  [[ "$assets" == "$ASSET" ]] || die "Release $tag must hold only $ASSET, it holds: $assets"

  # The stable link serves this exact file (the CDN can lag a few seconds).
  local try got=""
  for try in 1 2 3 4 5 6; do
    if curl -fsSL -o "$work/download.apk" "$STABLE_URL"; then
      got="$(sha256 "$work/download.apk")"
      [[ "$got" == "$sum" ]] && break
    fi
    sleep 5
  done
  [[ "$got" == "$sum" ]] || die "$STABLE_URL does not serve the new APK yet (sha256 ${got:-none}); check the Release page"

  log "published $tag from $sha"
  log "download: $STABLE_URL"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
