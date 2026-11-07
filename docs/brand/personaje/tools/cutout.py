import sys
from collections import deque
from PIL import Image, ImageFilter
src, out, prev = sys.argv[1], sys.argv[2], sys.argv[3]
im = Image.open(src).convert("RGB")
W, H = im.size
px = im.load()
def paper(c):
    r, g, b = c
    return min(r, g, b) > 200 and max(r, g, b) - min(r, g, b) < 40
bg = bytearray(W * H)
q = deque()
for x in range(W):
    for y in (0, H - 1):
        q.append((x, y))
for y in range(H):
    for x in (0, W - 1):
        q.append((x, y))
while q:
    x, y = q.popleft()
    i = y * W + x
    if bg[i] or not paper(px[x, y]):
        continue
    bg[i] = 1
    for nx, ny in ((x+1,y),(x-1,y),(x,y+1),(x,y-1)):
        if 0 <= nx < W and 0 <= ny < H and not bg[ny*W+nx]:
            q.append((nx, ny))
# close grain specks: blur the background mask and threshold
mask = Image.frombytes("L", (W, H), bytes(255 if v else 0 for v in bg))
mask = mask.filter(ImageFilter.MaxFilter(3)).filter(ImageFilter.MinFilter(3)).filter(ImageFilter.MedianFilter(5))
alpha = mask.point(lambda v: 255 - v).filter(ImageFilter.GaussianBlur(0.8))
rgba = im.copy(); rgba.putalpha(alpha)
rgba.save(out)
# preview on light and dark
pv = Image.new("RGB", (W * 2, H), (0xFB, 0xFB, 0xF2))
pv.paste(Image.new("RGB", (W, H), (0x18, 0x19, 0x18)), (W, 0))
pv.paste(rgba, (0, 0), rgba); pv.paste(rgba, (W, 0), rgba)
pv.resize((W, H // 2)).save(prev)
