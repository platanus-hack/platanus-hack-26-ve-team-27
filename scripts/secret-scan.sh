#!/usr/bin/env bash
# Scans the repo for committed secrets. Exits 1 on any hit, 0 when clean.
#
#   scripts/secret-scan.sh [repo-dir]     (default: the repo this script is in)
#
# Plain bash + git + grep, so it runs on macOS bash 3.2 and Linux with nothing
# to install. It scans tracked files plus untracked files that are not
# gitignored (git ls-files -co --exclude-standard), so a key in a new file is
# caught before it is ever added. Gitignored files (.env, local.properties) are
# skipped on purpose: that is where local keys belong. gitleaks is not used
# because `gitleaks dir` would read those gitignored files and fail every
# teammate's build.
#
# A line that contains the marker  secret-scan:allow  is never reported. Use it
# only for test fixtures and obvious placeholders, never for a real key.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(git -C "${1:-$SCRIPT_DIR}" rev-parse --show-toplevel)"
cd "$ROOT"

ALLOW_MARKER='secret-scan:allow'
# Values on assignment-style rules that are clearly not real keys.
PLACEHOLDER='your[_-]|example|placeholder|changeme|change[_-]me|replace[_-]?me|dummy|xxxx|<[^>]*>|\$\{|\$\(|\.\.\.|redacted|\*\*\*\*'

# Rules: name|grep-flags|placeholder-filter(y/n)|extended regex
RULES=(
  "elevenlabs-api-key|-E|n|sk_[0-9a-f]{32,}"
  "twilio-account-sid|-E|n|AC[0-9a-f]{32}"
  "twilio-api-key-sid|-E|n|SK[0-9a-f]{32}"
  "twilio-auth-token|-iE|y|auth_?token[a-z0-9_\"']*[[:space:]]*(:=|=|:)[[:space:]]*[\"']?[0-9a-f]{32}"
  "service-key-assignment|-iE|y|(elevenlabs|kapso|twilio)[a-z0-9_-]*(api_?key|key|token|secret)[a-z0-9_\"']*[[:space:]]*(=[[:space:]]*[\"']?|(:=|:)[[:space:]]*[\"'])[A-Za-z0-9_-]{16,}"
  "openai-anthropic-key|-E|n|sk-(ant-|proj-|svcacct-)?[A-Za-z0-9_-]{32,}"
  "aws-access-key-id|-E|n|(AKIA|ASIA)[0-9A-Z]{16}"
  "private-key-block|-E|n|-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY( BLOCK)?-----"
  "github-token|-E|n|(gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{60,})"
  "google-api-key|-E|n|AIza[0-9A-Za-z_-]{35}"
  "slack-token|-E|n|xox[abprs]-[A-Za-z0-9-]{10,}"
  "url-with-password|-iE|y|(postgres(ql)?|mysql|mongodb(\+srv)?|redis|amqp)://[^:/@[:space:]\"']+:[^@/[:space:]\"']{6,}@"
  "generic-secret-assignment|-iE|y|(api[_-]?key|secret|token|passw(or)?d)[a-z0-9_\"']*[[:space:]]*(:=|=|:)[[:space:]]*[\"'][^\"'[:space:]]{16,}[\"']"
)

# File list: tracked + untracked-not-ignored, existing, not binary blobs we
# know about. grep -I below also skips any other binary file.
FILES="$(mktemp "${TMPDIR:-/tmp}/secret-scan.XXXXXX")"
HITS="$(mktemp "${TMPDIR:-/tmp}/secret-scan.XXXXXX")"
trap 'rm -f "$FILES" "$HITS"' EXIT

git ls-files -co --exclude-standard -z | while IFS= read -r -d '' f; do
  [[ -f "$f" ]] || continue # deleted in the working tree
  case "$f" in
    gradle/wrapper/gradle-wrapper.jar) continue ;;
    *.png | *.jpg | *.jpeg | *.gif | *.webp | *.ico | *.pdf | *.jar | *.zip | *.ttf | *.otf | *.woff | *.woff2) continue ;;
  esac
  printf '%s\0' "$f"
done >"$FILES"

mask() { # keep the first 4 chars of a match, hide the rest
  local s="$1"
  printf '%s****(%d chars)' "${s:0:4}" "${#s}"
}

count=0
if [[ -s "$FILES" ]]; then
  for rule in "${RULES[@]}"; do
    name="${rule%%|*}"; rest="${rule#*|}"
    flags="${rest%%|*}"; rest="${rest#*|}"
    filter="${rest%%|*}"; regex="${rest#*|}"

    # grep exits 1 when a batch has no match; that is not an error here.
    xargs -0 grep -nHI "$flags" -e "$regex" -- <"$FILES" 2>/dev/null >"$HITS" || true

    while IFS= read -r hit; do
      file="${hit%%:*}"; rest="${hit#*:}"
      line="${rest%%:*}"; content="${rest#*:}"
      [[ "$content" == *"$ALLOW_MARKER"* ]] && continue
      match="$(printf '%s\n' "$content" | grep -o "$flags" -e "$regex" | head -n 1)"
      if [[ "$filter" == y ]] && printf '%s\n' "$match" | grep -qiE "$PLACEHOLDER"; then
        continue
      fi
      printf '  %s:%s  [%s]  %s\n' "$file" "$line" "$name" "$(mask "$match")"
      count=$((count + 1))
    done <"$HITS"
  done
fi

if ((count > 0)); then
  printf 'secret scan: FAILED, %d possible secret(s) found.\n' "$count" >&2
  printf 'Move each key to the vault and the Render env (see README "Secrets"), then remove it here.\n' >&2
  printf 'If a hit is a test fixture or placeholder, add the marker "%s" to that line.\n' "$ALLOW_MARKER" >&2
  exit 1
fi
printf 'secret scan: clean (%d files)\n' "$(tr -cd '\0' <"$FILES" | wc -c | tr -d ' ')"
