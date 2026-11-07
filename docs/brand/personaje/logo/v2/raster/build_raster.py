"""Candidate 1: the riso illustration itself as an Android adaptive icon.

Knocks the chartreuse circle out of mama-cutout.png (the adaptive background is
chartreuse anyway), frames the head inside the 66 dp safe zone and writes the
mipmap set, a 512 px Play icon and 48/108 px previews for the sheet.
Usage: python3 -I build_raster.py  (from logo/v2/raster)
"""
import pathlib
import numpy as np
from PIL import Image, ImageFilter, ImageDraw

SRC = "../../../mama-cutout.png"
CHART = (0xEF, 0xFE, 0x00)
# master px -> dp: the hair (1044 px wide) spans 74 dp, centred at (54, 52) dp.
K = 74 / 1044
CX, CY = 632, 517
DENS = {"mdpi": 108, "hdpi": 162, "xhdpi": 216, "xxhdpi": 324, "xxxhdpi": 432}


def knock_out(im):
    a = np.asarray(im).astype(int)
    r, g, b, al = a[..., 0], a[..., 1], a[..., 2], a[..., 3]
    chart = (r > 150) & (g > 160) & (b < 140) & (al > 0)
    keep = (al > 128) & ~chart
    m = Image.fromarray((keep * 255).astype(np.uint8))
    # drop the grain specks that float in the circle, keep real shapes
    m = m.filter(ImageFilter.MedianFilter(9))
    out = im.copy()
    out.putalpha(Image.fromarray(np.minimum(np.asarray(m), np.asarray(im.split()[3]))))
    return out


def src_for(char, smooth):
    if not smooth:
        return char
    rgb = char.convert("RGB").filter(ImageFilter.MedianFilter(7))
    out = rgb.convert("RGBA")
    out.putalpha(char.split()[3])
    return out


def foreground(char, px, smooth):
    """char: knocked-out RGBA master. Returns the 108 dp canvas at px pixels."""
    s = px / 108  # px per dp
    scale = K * s  # px per master px
    src = src_for(char, smooth)
    w = round(src.width * scale)
    small = src.resize((w, w), Image.LANCZOS)
    canvas = Image.new("RGBA", (px, px), (0, 0, 0, 0))
    ox = round(54 * s - CX * scale)
    oy = round(52 * s - CY * scale)
    canvas.alpha_composite(small, (ox, oy))
    return canvas


def masked(fg, bg_color, shape):
    """Preview the 108 dp canvas the way a launcher shows it: 72 dp mask, centred."""
    px = fg.width
    bg = Image.new("RGBA", (px, px), bg_color + (255,))
    bg.alpha_composite(fg)
    m = Image.new("L", (px, px), 0)
    d = ImageDraw.Draw(m)
    inset = round(px * 18 / 108)
    if shape == "circle":
        d.ellipse((inset, inset, px - inset, px - inset), fill=255)
    else:
        d.rounded_rectangle((inset, inset, px - inset, px - inset), radius=round(px * 0.2), fill=255)
    out = Image.new("RGBA", (px, px), (0, 0, 0, 0))
    out.paste(bg, mask=m)
    return out.crop((inset, inset, px - inset, px - inset))


master = Image.open(SRC).convert("RGBA")
char = knock_out(master)
char.save("mama-knockout.png")

for smooth in (False, True):
    tag = "smooth" if smooth else "grain"
    big = foreground(char, 1296, smooth)  # 12x, downsampled per density
    for name, px in DENS.items():
        fg = big.resize((px, px), Image.LANCZOS)
        fg.save(f"fg_{tag}_{px}.png")
        if smooth:  # the shipped set
            d = pathlib.Path(f"../android/mipmap-{name}"); d.mkdir(exist_ok=True)
            fg.save(d / "ic_launcher_foreground.png")
    # Play store icon: full-bleed 512, chartreuse behind, no mask.
    play = Image.new("RGBA", (512, 512), CHART + (255,))
    ps = 440 / 1044  # hair spans 440 of 512, head centred a little high
    pw = round(src_for(char, smooth).width * ps)
    play.alpha_composite(src_for(char, smooth).resize((pw, pw), Image.LANCZOS), (round(256 - CX * ps), round(240 - CY * ps)))
    play.convert("RGB").save(f"play_{tag}_512.png")
    # Previews as a launcher would show them: 108 px and 48 px icons.
    for size in (108, 48):
        canvas_px = round(size * 108 / 72)
        fg = big.resize((canvas_px, canvas_px), Image.LANCZOS)
        for shape in ("circle", "squircle"):
            masked(fg, CHART, shape).save(f"icon_{tag}_{shape}_{size}.png")
print("ok")
