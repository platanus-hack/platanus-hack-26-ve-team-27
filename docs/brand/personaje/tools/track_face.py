"""Track a driving video's face into per-frame puppet signals (runs fully offline).

Usage: python tools/track_face.py driving.mp4 face_landmarker.task signals.json

Writes one record per video frame with MediaPipe blendshapes (jawOpen, smile,
blink, brow raise) and head pose (roll in degrees, normalised x/y of the face
centre). Frames without a face are marked invalid so the renderer can hold
the last good values.
"""
import json
import math
import sys

import cv2
import mediapipe as mp
from mediapipe.tasks.python import BaseOptions
from mediapipe.tasks.python import vision

video_path, model_path, out_path = sys.argv[1], sys.argv[2], sys.argv[3]

options = vision.FaceLandmarkerOptions(
    base_options=BaseOptions(model_asset_path=model_path),
    running_mode=vision.RunningMode.VIDEO,
    num_faces=1,
    output_face_blendshapes=True,
    output_facial_transformation_matrixes=True,
)
landmarker = vision.FaceLandmarker.create_from_options(options)

cap = cv2.VideoCapture(video_path)
fps = cap.get(cv2.CAP_PROP_FPS) or 30.0
frames = []
i = 0
while True:
    ok, bgr = cap.read()
    if not ok:
        break
    rgb = cv2.cvtColor(bgr, cv2.COLOR_BGR2RGB)
    image = mp.Image(image_format=mp.ImageFormat.SRGB, data=rgb)
    result = landmarker.detect_for_video(image, int(i * 1000 / fps))
    rec = {"t": i / fps, "valid": False}
    if result.face_blendshapes:
        bs = {c.category_name: c.score for c in result.face_blendshapes[0]}
        lm = result.face_landmarks[0]
        # Roll from the outer eye corners (33 = subject's right, 263 = left).
        h, w = rgb.shape[:2]
        dx = (lm[263].x - lm[33].x) * w
        dy = (lm[263].y - lm[33].y) * h
        roll = math.degrees(math.atan2(dy, dx))
        xs = [p.x for p in lm]
        ys = [p.y for p in lm]
        rec.update(
            valid=True,
            jaw=bs.get("jawOpen", 0.0),
            mouth_close=bs.get("mouthClose", 0.0),
            funnel=bs.get("mouthFunnel", 0.0) + bs.get("mouthPucker", 0.0),
            smile=(bs.get("mouthSmileLeft", 0.0) + bs.get("mouthSmileRight", 0.0)) / 2,
            blink=(bs.get("eyeBlinkLeft", 0.0) + bs.get("eyeBlinkRight", 0.0)) / 2,
            brow=max(bs.get("browInnerUp", 0.0),
                     (bs.get("browOuterUpLeft", 0.0) + bs.get("browOuterUpRight", 0.0)) / 2),
            roll=roll,
            cx=(min(xs) + max(xs)) / 2,
            cy=(min(ys) + max(ys)) / 2,
            size=max(ys) - min(ys),
        )
    frames.append(rec)
    i += 1

json.dump({"fps": fps, "frames": frames}, open(out_path, "w"))
valid = sum(f["valid"] for f in frames)
print(f"{len(frames)} frames at {fps:.2f} fps, face found in {valid}")
