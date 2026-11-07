"""Slice a pose sheet of chartreuse circles into aligned transparent WebPs.

Usage: python3 -I tools/slice_sheet.py sheet.png out_dir name1 name2 ...
Names are assigned to circles in reading order (top-left to bottom-right).
Each output is a square canvas with the circle scaled to the same diameter,
so swapping poses in the app does not make the character jump.
"""
import sys
import numpy as np
from PIL import Image, ImageDraw, ImageFilter

CANVAS = 512
DIAMETER = 492  # leaves a small margin for the grainy edge

sheet_path, out_dir, names = sys.argv[1], sys.argv[2], sys.argv[3:]
im = Image.open(sheet_path).convert("RGB")
a = np.asarray(im).astype(int)
r, g, b = a[..., 0], a[..., 1], a[..., 2]
paper = (np.minimum(np.minimum(r, g), b) > 200) & ((np.maximum(np.maximum(r, g), b) - np.minimum(np.minimum(r, g), b)) < 40)
ink = ~paper

# Find circles: columns/rows of ink split by paper gutters.
def runs(profile, min_len):
    out, start = [], None
    for i, v in enumerate(profile):
        if v and start is None:
            start = i
        elif not v and start is not None:
            if i - start >= min_len:
                out.append((start, i))
            start = None
    if start is not None and len(profile) - start >= min_len:
        out.append((start, len(profile)))
    return out

rows = runs(ink.sum(axis=1) > 20, 100)
ncols = len(names) // max(len(rows), 1)
boxes = []
for y0, y1 in rows:
    prof = ink[y0:y1].sum(axis=0)
    # Circles may touch: split at the thinnest column near each equal division.
    span = runs(prof > 20, 100)
    left, right = span[0][0], span[-1][1]
    width = right - left
    cuts = [left]
    for k in range(1, ncols):
        c = left + width * k // ncols
        lo, hi = c - width // (4 * ncols), c + width // (4 * ncols)
        cuts.append(lo + int(np.argmin(prof[lo:hi])))
    cuts.append(right)
    for x0, x1 in zip(cuts, cuts[1:]):
        boxes.append((x0, y0, x1, y1))
if len(boxes) != len(names):
    sys.exit(f"found {len(boxes)} circles, got {len(names)} names")

def hull(points):
    pts = sorted(set(points))
    def cross(o, p, q):
        return (p[0]-o[0])*(q[1]-o[1]) - (p[1]-o[1])*(q[0]-o[0])
    lower, upper = [], []
    for p in pts:
        while len(lower) >= 2 and cross(lower[-2], lower[-1], p) <= 0:
            lower.pop()
        lower.append(p)
    for p in reversed(pts):
        while len(upper) >= 2 and cross(upper[-2], upper[-1], p) <= 0:
            upper.pop()
        upper.append(p)
    return lower[:-1] + upper[:-1]

for (x0, y0, x1, y1), name in zip(boxes, names):
    sub = ink[y0:y1, x0:x1]
    # Grain specks and the neighbour circle's rim also count as ink, so work on a
    # 4x downscaled density map and keep only its largest solid component.
    h4, w4 = sub.shape[0] // 4, sub.shape[1] // 4
    dens = sub[:h4 * 4, :w4 * 4].reshape(h4, 4, w4, 4).mean(axis=(1, 3)) > 0.5
    seen = np.zeros_like(dens)
    best = []
    for sy, sx in zip(*np.nonzero(dens)):
        if seen[sy, sx]:
            continue
        comp, stack = [], [(sy, sx)]
        seen[sy, sx] = True
        while stack:
            cy, cx = stack.pop()
            comp.append((cx * 4 + 2, cy * 4 + 2))
            for ny, nx in ((cy+1, cx), (cy-1, cx), (cy, cx+1), (cy, cx-1)):
                if 0 <= ny < h4 and 0 <= nx < w4 and dens[ny, nx] and not seen[ny, nx]:
                    seen[ny, nx] = True
                    stack.append((ny, nx))
        if len(comp) > len(best):
            best = comp
    poly = hull(best)
    crop = im.crop((x0, y0, x1, y1))
    mask = Image.new("L", crop.size, 0)
    ImageDraw.Draw(mask).polygon(poly, fill=255)
    mask = mask.filter(ImageFilter.MinFilter(5)).filter(ImageFilter.GaussianBlur(1))
    rgba = crop.copy()
    rgba.putalpha(mask)
    bbox = mask.getbbox()
    rgba = rgba.crop(bbox)
    scale = DIAMETER / max(rgba.size)
    rgba = rgba.resize((round(rgba.width * scale), round(rgba.height * scale)), Image.LANCZOS)
    canvas = Image.new("RGBA", (CANVAS, CANVAS), (0, 0, 0, 0))
    canvas.paste(rgba, ((CANVAS - rgba.width) // 2, (CANVAS - rgba.height) // 2), rgba)
    canvas.save(f"{out_dir}/{name}.webp", "WEBP", quality=90, method=6)
    print(name, (x0, y0, x1, y1), "->", rgba.size)
