#!/bin/sh
# Render sheet.html to sheet.png with headless Chrome (no rsvg on this machine).
set -eu
cd "$(dirname "$0")"
H="${1:-1500}"
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --hide-scrollbars \
  --force-device-scale-factor=2 --window-size=1800,"$H" --virtual-time-budget=3000 \
  --screenshot="$PWD/sheet.png" "file://$PWD/sheet.html" 2>/dev/null
ls -la sheet.png
