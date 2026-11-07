"""Cutout-puppet renderer: Mamá talks, blinks and tilts like the driving video.

Usage:
  python tools/mama_puppet.py sprites            # writes a sprite preview sheet
  python tools/mama_puppet.py render driving.mp4 signals.json out_dir

Everything runs locally. The face edits work on mama-cutout.png (1254 px,
transparent background): the original lips are the closed mouth, open mouths
are drawn over a repainted patch of skin, and a blink erases the irises so
the upper lids read as closed eyes. Coordinates below were measured on that
image; regenerate them if the master art changes.
"""
import json
import math
import os
import subprocess
import sys

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFilter

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
CUTOUT = os.path.join(ROOT, "mama-cutout.png")

PAPER = (0xFB, 0xFB, 0xF2)
INK = np.array([57, 56, 50])           # lips as printed (measured on the master)
MOUTH_DARK = np.array([0x14, 0x11, 0x11])  # inside of the mouth, darker than the lips so they separate
TEETH = np.array([0xF4, 0xEF, 0xDC])      # paper tone, a touch darker than skin

# Mouth geometry (image pixels): corners and the seam's sag toward the chin.
LEFT_CORNER = np.array([676.0, 609.0])
RIGHT_CORNER = np.array([786.0, 569.0])
SEAM_SAG = 9.0
MOUTH_ERASE_BOX = (664, 561, 794, 628)    # never above y=561: the nose sits there

# Irises sit under the thick upper lid strokes; erasing them closes the eyes.
IRIS_POLYS = [
    [(627, 503), (648, 499), (671, 502), (671, 541), (627, 541)],
    [(777, 447), (816, 445), (818, 484), (777, 484)],
]

SS = 4  # supersampling for drawn shapes


def load_base():
    im = Image.open(CUTOUT).convert("RGBA")
    return np.asarray(im).astype(np.float32)


SKIN = np.array([246.0, 240.0, 218.0])  # mean of clean skin on the chin


def skin_fill(rgb, mask, flat=False):
    """Repaint mask pixels with skin (inpainted or flat colour) plus paper grain."""
    m = (mask > 0).astype(np.uint8) * 255
    if flat:
        filled = np.broadcast_to(SKIN, rgb.shape).astype(np.float32)
    else:
        bgr = cv2.cvtColor(rgb.astype(np.uint8), cv2.COLOR_RGB2BGR)
        filled = cv2.inpaint(bgr, m, 9, cv2.INPAINT_TELEA)
        filled = cv2.cvtColor(filled, cv2.COLOR_BGR2RGB).astype(np.float32)
    # Grain: high-pass of a clean skin patch on the chin, tiled with random offsets.
    patch = rgb[632:668, 652:740].copy()
    hp = patch - cv2.GaussianBlur(patch, (0, 0), 3)
    h, w = rgb.shape[:2]
    reps = (h // hp.shape[0] + 2, w // hp.shape[1] + 2, 1)
    tile = np.tile(hp, reps)
    oy, ox = np.random.default_rng(7).integers(0, hp.shape[0]), np.random.default_rng(8).integers(0, hp.shape[1])
    tile = tile[oy:oy + h, ox:ox + w]
    soft = cv2.GaussianBlur(m.astype(np.float32) / 255.0, (0, 0), 0.8 if flat else 1.2)[..., None]
    out = rgb * (1 - soft) + (filled + tile) * soft
    return out


def dark_mask_in_box(rgb, box, thresh=150, grow=3):
    x0, y0, x1, y1 = box
    lum = rgb[..., :3].mean(axis=2)
    mask = np.zeros(lum.shape, np.uint8)
    sub = (lum[y0:y1, x0:x1] < thresh).astype(np.uint8)
    mask[y0:y1, x0:x1] = sub
    if grow:
        mask = cv2.dilate(mask, np.ones((2 * grow + 1, 2 * grow + 1), np.uint8))
    return mask


def seam_frame(t):
    """Point on the lip seam and the unit normal pointing toward the chin."""
    d = RIGHT_CORNER - LEFT_CORNER
    tangent = d / np.linalg.norm(d)
    normal = np.array([-tangent[1], tangent[0]])
    if normal[1] < 0:
        normal = -normal
    p = LEFT_CORNER + d * t + normal * SEAM_SAG * math.sin(math.pi * t)
    return p, normal


def lip_profile(t):
    """0 at the corners, 1 at the fullest point (t = 0.6)."""
    p, q = 1.2, 0.8
    peak = (p / (p + q)) ** p * (q / (p + q)) ** q
    return (t ** p) * ((1 - t) ** q) / peak


def mouth_shapes(opening, narrow=0.0):
    """Polygons (image px) for an open mouth: upper lip, lower lip, cavity, teeth."""
    ts = np.linspace(0, 1, 60)
    # Rounder "o": pull both corners toward the centre.
    squeeze = 0.18 * narrow
    up_in, lo_in, up_out, lo_out = [], [], [], []
    for t in ts:
        tt = squeeze + t * (1 - 2 * squeeze)
        p, n = seam_frame(tt)
        bell = math.sin(math.pi * t) ** (0.75 - 0.35 * narrow)
        a = opening * 0.32 * bell
        b = opening * 0.68 * bell
        # The lips are fullest right of centre (3/4 view), like the original.
        full = lip_profile(t) ** 0.55
        bow = 3.5 * math.exp(-((t - 0.68) / 0.05) ** 2)
        thu = 15.0 * full - bow
        thl = 21.0 * full
        up_in.append(p - n * a)
        lo_in.append(p + n * b)
        up_out.append(p - n * (a + thu))
        lo_out.append(p + n * (b + thl))
    upper = up_out + up_in[::-1]
    lower = lo_in + lo_out[::-1]
    cavity = up_in + lo_in[::-1]
    teeth_h = min(7.0, opening * 0.45)
    teeth = []
    for t, u in zip(ts, up_in):
        _, n = seam_frame(t)
        teeth.append(u + n * teeth_h * math.sin(math.pi * t) ** 0.4)
    teeth = up_in + teeth[::-1]
    return upper, lower, cavity, teeth


def draw_mouth(rgb, opening, narrow=0.0):
    """Erase the original lips and draw an open mouth in the same printed look."""
    mask = dark_mask_in_box(rgb, MOUTH_ERASE_BOX)
    out = skin_fill(rgb, mask)
    x0, y0, x1, y1 = MOUTH_ERASE_BOX[0] - 12, MOUTH_ERASE_BOX[1] - 12, MOUTH_ERASE_BOX[2] + 12, MOUTH_ERASE_BOX[3] + 30
    w, h = (x1 - x0) * SS, (y1 - y0) * SS
    layers = {}
    for name in ("lips", "cavity", "teeth"):
        layers[name] = Image.new("L", (w, h), 0)
    upper, lower, cavity, teeth = mouth_shapes(opening, narrow)

    def pts(poly):
        return [((p[0] - x0) * SS, (p[1] - y0) * SS) for p in poly]

    dl = ImageDraw.Draw(layers["lips"])
    dl.polygon(pts(upper), fill=255)
    dl.polygon(pts(lower), fill=255)
    # Corner details from the original: a round dot on the left, a tiny upturned tick on the right.
    if narrow == 0:
        lx, ly = (LEFT_CORNER - [x0, y0]) * SS
        dl.ellipse([lx - 5 * SS, ly - 5 * SS, lx + 4 * SS, ly + 4 * SS], fill=255)
        rx, ry = (RIGHT_CORNER - [x0, y0]) * SS
        dl.line([(rx - 4 * SS, ry + 3 * SS), (rx + 2 * SS, ry - 2 * SS)], fill=255, width=5 * SS)
        dl.ellipse([rx, ry - 5 * SS, rx + 6 * SS, ry + 1 * SS], fill=255)
    ImageDraw.Draw(layers["cavity"]).polygon(pts(cavity), fill=255)
    ImageDraw.Draw(layers["teeth"]).polygon(pts(teeth), fill=255)

    def down(img):
        return np.asarray(img.resize((x1 - x0, y1 - y0), Image.LANCZOS)).astype(np.float32)[..., None] / 255.0

    lips, cav, tee = down(layers["lips"]), down(layers["cavity"]), down(layers["teeth"])
    tee = np.minimum(tee, cav)
    rng = np.random.default_rng(int(opening * 10 + narrow * 100))
    region = out[y0:y1, x0:x1, :3]
    # Uneven ink density plus a few paper specks, like the printed lips.
    density = 1.0 - 0.30 * cv2.GaussianBlur(rng.random(region.shape[:2]).astype(np.float32), (0, 0), 2)[..., None]
    specks = (rng.random(region.shape[:2]) > 0.992).astype(np.float32)[..., None] * 0.6
    ink = INK * density + (np.array(PAPER) - INK) * specks
    region = region * (1 - lips) + ink * lips
    region = region * (1 - cav) + (MOUTH_DARK * density) * cav
    region = region * (1 - tee) + TEETH * tee
    out[y0:y1, x0:x1, :3] = region
    return out


def close_eyes(rgb):
    mask = np.zeros(rgb.shape[:2], np.uint8)
    lum = rgb[..., :3].mean(axis=2)
    for poly in IRIS_POLYS:
        poly_mask = np.zeros_like(mask)
        cv2.fillPoly(poly_mask, [np.array(poly, np.int32)], 1)
        mask |= ((lum < 170) & (poly_mask > 0)).astype(np.uint8)
    mask = cv2.dilate(mask, np.ones((5, 5), np.uint8))
    # Keep the fill inside the polygons so the lid strokes above stay crisp.
    limit = np.zeros_like(mask)
    for poly in IRIS_POLYS:
        cv2.fillPoly(limit, [np.array(poly, np.int32)], 1)
    mask &= cv2.dilate(limit, np.ones((3, 3), np.uint8))
    return skin_fill(rgb, mask, flat=True)


MOUTH_LEVELS = {0: None, 1: 7.0, 2: 14.0, 3: 22.0}


def build_sprites():
    """All face variants: (mouth level, rounded, eyes closed) -> RGBA float image."""
    base = load_base()
    sprites = {}
    for blink in (False, True):
        face = base.copy()
        if blink:
            face[..., :3] = close_eyes(face[..., :3])
        for level, opening in MOUTH_LEVELS.items():
            for narrow in ((0.0, 1.0) if opening else (0.0,)):
                img = face.copy()
                if opening:
                    img[..., :3] = draw_mouth(face[..., :3], opening, narrow)
                sprites[(level, narrow > 0, blink)] = img
    return sprites


def to_image(arr):
    return Image.fromarray(np.clip(arr, 0, 255).astype(np.uint8), "RGBA")


def preview(sprites, out_path):
    keys = sorted(sprites)
    cells = []
    for k in keys:
        crop = to_image(sprites[k]).crop((520, 380, 880, 680))
        bg = Image.new("RGBA", crop.size, PAPER + (255,))
        bg.alpha_composite(crop)
        d = ImageDraw.Draw(bg)
        d.text((6, 6), f"m{k[0]} {'o' if k[1] else ''} {'blink' if k[2] else ''}", fill=(255, 0, 0, 255))
        cells.append(bg)
    cols = 5
    rows = math.ceil(len(cells) / cols)
    sheet = Image.new("RGBA", (cols * 360, rows * 300), (255, 255, 255, 255))
    for i, c in enumerate(cells):
        sheet.paste(c, ((i % cols) * 360, (i // cols) * 300))
    sheet.convert("RGB").save(out_path)


# ------------------------------------------------------------------ driving

def audio_envelope(video, fps):
    raw = subprocess.run(
        ["ffmpeg", "-v", "error", "-i", video, "-ac", "1", "-ar", "16000", "-f", "s16le", "-"],
        capture_output=True, check=True).stdout
    pcm = np.frombuffer(raw, np.int16).astype(np.float32) / 32768.0
    hop = int(16000 / fps)
    n = len(pcm) // hop
    rms = np.sqrt(np.mean(pcm[: n * hop].reshape(n, hop) ** 2, axis=1) + 1e-12)
    db = 20 * np.log10(rms)
    floor, top = np.percentile(db, 20), np.percentile(db, 97)
    return np.clip((db - floor - 4) / max(top - floor - 4, 1e-3), 0, 1)


def ema(x, alpha):
    out = np.empty_like(x)
    acc = x[0]
    for i, v in enumerate(x):
        acc = alpha * v + (1 - alpha) * acc
        out[i] = acc
    return out


def driving_tracks(video, signals_path, out_fps):
    data = json.load(open(signals_path))
    fps = data["fps"]
    frames = data["frames"]
    keys = ["jaw", "funnel", "blink", "roll", "cx", "cy"]
    tracks = {k: [] for k in keys}
    last = None
    for f in frames:
        if f["valid"]:
            last = f
        src = last or next(x for x in frames if x["valid"])
        for k in keys:
            tracks[k].append(src[k])
    tracks = {k: np.array(v, np.float32) for k, v in tracks.items()}
    # The last frames show a hand over the lens: hold the pose from before it.
    audio = audio_envelope(video, fps)
    n = min(len(audio), len(tracks["jaw"]))
    jaw = np.clip((tracks["jaw"][:n] - 0.02) / 0.22, 0, 1)
    mouth = np.maximum(jaw, audio[:n] * 0.95)
    mouth = ema(mouth, 0.6)
    blink_base = np.median(tracks["blink"])
    blink = tracks["blink"][:n] - blink_base
    roll = ema(tracks["roll"][:n], 0.25)
    roll = roll - np.median(roll)
    cx = ema(tracks["cx"][:n], 0.2) - np.median(tracks["cx"])
    cy = ema(tracks["cy"][:n], 0.2) - np.median(tracks["cy"])
    funnel = ema(tracks["funnel"][:n], 0.5)
    duration = n / fps
    out = []
    for i in range(int(duration * out_fps)):
        t0, t1 = i / out_fps, (i + 1) / out_fps
        a, b = int(t0 * fps), max(int(t1 * fps), int(t0 * fps) + 1)
        m = float(mouth[a:b].mean())
        level = 0 if m < 0.22 else 1 if m < 0.5 else 2 if m < 0.78 else 3
        out.append({
            "level": level,
            "round": bool(level >= 2 and funnel[a:b].mean() > 0.28),
            "blink": bool(blink[a:b].max() > 0.32),
            "roll": float(np.clip(roll[a:b].mean() * 0.35, -3.5, 3.5)),
            "dx": float(np.clip(cx[a:b].mean() * 120, -18, 18)),
            "dy": float(np.clip(cy[a:b].mean() * 120, -12, 12)),
        })
    # A real blink lasts 1-2 frames at 12 fps; longer runs read as dozing off.
    run = 0
    for rec in out:
        run = run + 1 if rec["blink"] else 0
        if run > 2:
            rec["blink"] = False
    return out, duration


def render(video, signals_path, out_dir, out_fps=12):
    os.makedirs(out_dir, exist_ok=True)
    tracks, duration = driving_tracks(video, signals_path, out_fps)
    sprites = build_sprites()
    cache = {k: to_image(v) for k, v in sprites.items()}
    rng = np.random.default_rng(3)
    for name, (W, H, size) in {"vertical": (1080, 1920, 1000), "square": (1080, 1080, 980)}.items():
        frame_dir = os.path.join(out_dir, f"frames_{name}")
        os.makedirs(frame_dir, exist_ok=True)
        for i, tr in enumerate(tracks):
            face = cache[(tr["level"], tr["round"], tr["blink"])]
            fig = face.resize((size, size), Image.LANCZOS)
            # Tilt around the bottom centre so the blouse stays planted.
            pivot = (size / 2, size * 0.98)
            fig = fig.rotate(-tr["roll"], resample=Image.BICUBIC, center=pivot)
            canvas = Image.new("RGBA", (W, H), PAPER + (255,))
            x = (W - size) // 2 + int(tr["dx"])
            y = (H - size) // 2 + int(tr["dy"])
            canvas.alpha_composite(fig, (x, y))
            arr = np.asarray(canvas.convert("RGB")).astype(np.float32)
            grain = rng.normal(0, 5.5, arr.shape[:2]).astype(np.float32)[..., None]
            arr = np.clip(arr + grain, 0, 255).astype(np.uint8)
            Image.fromarray(arr).save(os.path.join(frame_dir, f"{i:05d}.png"))
        print(name, len(tracks), "frames")
    json.dump(tracks, open(os.path.join(out_dir, "tracks.json"), "w"))
    return duration


if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "sprites":
        preview(build_sprites(), sys.argv[2])
    elif cmd == "render":
        render(sys.argv[2], sys.argv[3], sys.argv[4])
