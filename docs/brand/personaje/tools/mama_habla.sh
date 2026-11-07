#!/bin/sh
# Mamá talks: drive the riso puppet with a selfie video, fully offline.
# Usage: tools/mama_habla.sh driving.mp4 [out_dir]
# Writes mama-hablando-vertical.mp4 (1080x1920), mama-hablando-square.mp4
# (1080x1080) and mariana-vs-mama.mp4 (1920x1080, driver left, Mamá right).
# The Python env and the MediaPipe face model are cached outside the project
# (override with MAMA_CACHE). Nothing is uploaded anywhere.
set -eu

DRIVING=$1
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=${2:-$HERE/../video}
CACHE=${MAMA_CACHE:-$HOME/.cache/mama-puppet}
VENV=$CACHE/venv
MODEL=$CACHE/face_landmarker.task
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$OUT" "$CACHE"
if [ ! -x "$VENV/bin/python" ]; then
  uv venv -q -p 3.12 "$VENV"
  uv pip install -q -p "$VENV/bin/python" mediapipe opencv-python numpy pillow
fi
if [ ! -f "$MODEL" ]; then
  curl -sSfL -o "$MODEL" \
    https://storage.googleapis.com/mediapipe-models/face_landmarker/face_landmarker/float16/latest/face_landmarker.task
fi

"$VENV/bin/python" "$HERE/track_face.py" "$DRIVING" "$MODEL" "$WORK/signals.json" 2>/dev/null
"$VENV/bin/python" "$HERE/mama_puppet.py" render "$DRIVING" "$WORK/signals.json" "$WORK"

encode() { # frames_dir output
  ffmpeg -v error -y -framerate 12 -i "$1/%05d.png" -i "$DRIVING" \
    -map 0:v -map 1:a? -c:v libx264 -crf 23 -preset slow -pix_fmt yuv420p \
    -r 24 -c:a aac -b:a 160k -shortest -movflags +faststart "$2"
}
encode "$WORK/frames_vertical" "$OUT/mama-hablando-vertical.mp4"
encode "$WORK/frames_square" "$OUT/mama-hablando-square.mp4"

# Side by side for the pitch's "behind the scenes": the driver left, Mamá right.
ffmpeg -v error -y -i "$DRIVING" -framerate 12 -i "$WORK/frames_square/%05d.png" \
  -filter_complex "[0:v]scale=-2:1080,pad=960:1080:(ow-iw)/2:0:color=0xFBFBF2,fps=24[l];[1:v]scale=960:960,pad=960:1080:0:60:color=0xFBFBF2,fps=24[r];[l][r]hstack=inputs=2[v]" \
  -map "[v]" -map 0:a? -c:v libx264 -crf 23 -preset slow -pix_fmt yuv420p \
  -c:a aac -b:a 160k -shortest -movflags +faststart "$OUT/mariana-vs-mama.mp4"

echo "done: $OUT"
