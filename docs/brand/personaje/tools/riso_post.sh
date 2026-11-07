#!/bin/sh
# Post-process a Runway clip into the riso look: 12 fps steps, fresh grain, small file.
# Usage: tools/riso_post.sh input.mp4 output.mp4
set -eu
ffmpeg -y -i "$1" -vf "fps=12,noise=alls=7:allf=t,format=yuv420p" -c:v libx264 -crf 20 -preset slow -movflags +faststart -an "$2"
