"""Generate the MAMÁ logo candidates (SVG files) and the comparison sheet (HTML → PNG).

Usage: python3 build_sheet.py            # writes svg/*.svg, sheet.html
Then render sheet.html with headless Chrome (see render.sh).

Every candidate is a function (ink, lime, paper, mode) -> svg body in a 100x100
box, so the same geometry produces the full-colour, dark, mono and launcher
variants.  mode is "full" | "dark" | "mono" | "icon".
"""
from pathlib import Path

INK, LIME, PAPER, OLIVE = "#181918", "#EFFE00", "#FBFBF2", "#4A5400"
HERE = Path(__file__).parent
SVG_DIR = HERE / "svg"
SVG_DIR.mkdir(exist_ok=True)
# The app's Lexend files, as a URL relative to sheet.html (this folder).
FONT_DIR = "../../../../app/src/main/res/font"

# ------------------------------------------------------------------ letterforms
# Geometric caps on a 100-unit cap height, 18-unit stems. Both 90 wide.
M_PATH = "M0,0 H22 L45,46 L68,0 H90 V100 H72 V30 L50,74 H40 L18,30 V100 H0 Z"
A_PATH = "M36,0 H54 L90,100 H71 L62,75 H28 L19,100 H0 Z M33,58 H57 L45,24 Z"  # even-odd


def letter(path, x, fill, scale=1.0, y=0.0):
    return (f'<path d="{path}" fill="{fill}" fill-rule="evenodd" '
            f'transform="translate({x:.2f},{y:.2f}) scale({scale:.4f})"/>')


def wordmark(ink, lime, accent="dot", misreg=None, scale=1.0, x0=0.0, y0=0.0):
    """MAMÁ as paths. accent: "dot" (chartreuse circle) or "acute" (ink tick).
    misreg: (dx, dy) draws a lime copy behind, offset (riso misregistration)."""
    gap, w = 14, 90
    xs = [x0 + (w + gap) * i * scale for i in range(4)]
    paths = [M_PATH, A_PATH, M_PATH, A_PATH]
    out = []
    if misreg:
        dx, dy = misreg
        for p, x in zip(paths, xs):
            out.append(letter(p, x + dx, lime, scale, y0 + dy))
        out.append(f'<path d="M58,-30 L72,-44 L84,-32 L70,-18 Z" fill="{lime}" '
                   f'transform="translate({xs[3] + dx:.2f},{y0 + dy:.2f}) scale({scale:.4f})"/>')
    for p, x in zip(paths, xs):
        out.append(letter(p, x, ink, scale, y0))
    ax = xs[3]
    if accent == "dot":
        out.append(f'<circle cx="{ax + 60 * scale:.2f}" cy="{y0 - 26 * scale:.2f}" r="{12 * scale:.2f}" fill="{lime}"/>')
    else:  # acute: a short thick tick leaning right, same stem weight
        out.append(f'<path d="M58,-30 L72,-44 L84,-32 L70,-18 Z" fill="{ink}" '
                   f'transform="translate({ax:.2f},{y0:.2f}) scale({scale:.4f})"/>')
    return "\n".join(out)


WORD_W, WORD_H = 90 * 4 + 14 * 3, 100  # 402 x 100 (accent rises ~38 above)

# ------------------------------------------------------------------ candidates
# Each returns the body of a 100x100 SVG.


def c1_mark(ink, lime, paper, mode):
    """Á alone: the A with the chartreuse circle as its accent (the halo / hoop)."""
    s = 0.66
    a_fill = paper if mode in ("dark", "icon") else ink
    dot = ink if mode == "mono" else lime
    body = letter(A_PATH, 50 - 45 * s, a_fill, s, 100 - 100 * s - 2)
    body += f'\n<circle cx="{50 + 14 * s:.2f}" cy="{100 - 100 * s - 2 - 22 * s:.2f}" r="{12 * s:.2f}" fill="{dot}"/>'
    return body


# Cameo: Mamá in profile, one charcoal silhouette. Bouffant on top, the blowout
# flipping out at the back, a short neck. Facing right.
PROFILE = ("M42,6 C58,6 66,14 65,26 C67,30 69,32 68,36 C66,38 66,40 68,42 "
           "C72,44 75,46 73,48 C70,50 68,50 68,52 C72,53 72,56 68,57 C71,58 72,61 69,62 "
           "C72,64 71,67 66,69 C62,71 58,72 57,76 C56,82 58,88 60,94 L38,94 "
           "C40,88 38,82 36,76 C28,84 14,88 5,80 C12,74 20,70 22,62 "
           "C12,56 8,44 12,32 C16,18 28,6 42,6 Z")
# One cat-eye lens seen from the side, resting on the hair at the front.
SIDE_LENS = "M44,14 C49,5 63,6 68,17 C62,21 50,21 44,14 Z"
HOOP = (55, 64)


def c2_mark(ink, lime, paper, mode):
    """Cameo: Mamá's profile with the sunglasses on her head and a hoop earring."""
    if mode == "mono":
        circle = f'<circle cx="50" cy="50" r="45" fill="none" stroke="{ink}" stroke-width="5"/>'
        lens = f'<path d="{SIDE_LENS}" fill="{paper}"/>'
        hoop_c = paper
        clip = 'clip-path="circle(45 at 50 50)"'
    else:
        circle = "" if mode == "icon" else f'<circle cx="50" cy="50" r="48" fill="{lime}"/>'
        lens = f'<path d="{SIDE_LENS}" fill="{lime}" stroke="{paper}" stroke-width="2.4" stroke-linejoin="round"/>'
        hoop_c = lime
        clip = 'clip-path="circle(48 at 50 50)"' if mode != "icon" else ""
    head = f'<path d="{PROFILE}" fill="{ink}" {clip}/>'
    hoop = f'<circle cx="{HOOP[0]}" cy="{HOOP[1]}" r="5.5" fill="none" stroke="{hoop_c}" stroke-width="2.8"/>'
    return "\n".join([circle, head, lens, hoop])


BUBBLE = "M50,2 C76,2 95,22 95,47 C95,72 76,92 50,92 C45,92 40,91 36,90 L13,97 L24,78 C17,70 5,60 5,47 C5,22 24,2 50,2 Z"
SMILE = "M31,47 C37,67 63,67 69,47"


def c3_mark(ink, lime, paper, mode):
    """Llamada: a speech bubble (the call) whose only content is a smile."""
    if mode == "mono":
        bubble, fg = ink, paper
    else:
        bubble, fg = lime, ink
    return (f'<path d="{BUBBLE}" fill="{bubble}"/>'
            f'<path d="{SMILE}" fill="none" stroke="{fg}" stroke-width="10" stroke-linecap="round"/>')


def c4_mark(ink, lime, paper, mode):
    """Riso misregistration: a bold M printed twice, the chartreuse pass off-register."""
    s = 0.72
    x, y = 50 - 45 * s, 50 - 50 * s
    ink_fill = paper if mode in ("dark", "icon") else ink
    out = []
    if mode == "mono":
        out.append(letter(M_PATH, x + 5, "none", s, y + 5).replace('fill="none"', f'fill="none" stroke="{ink}" stroke-width="2.5"'))
    else:
        out.append(letter(M_PATH, x + 5, lime, s, y + 5))
    out.append(letter(M_PATH, x, ink_fill, s, y))
    return "\n".join(out)


CANDIDATES = [
    ("1", "Á con halo", "Wordmark: el acento de la Á es el círculo chartreuse (el halo de Mamá, su argolla).", c1_mark),
    ("2", "Camafeo", "Mamá de perfil, como el camafeo de una señora: el blower, los lentes en la cabeza y la argolla.", c2_mark),
    ("3", "Llamada", "Una burbuja de llamada cuyo único contenido es una sonrisa.", c3_mark),
    ("4", "Riso M", "Una M impresa dos veces: la pasada chartreuse quedó fuera de registro.", c4_mark),
]


def svg(body, w=100, h=100, vb="0 0 100 100", bg=None):
    rect = f'<rect width="100%" height="100%" fill="{bg}"/>' if bg else ""
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vb}" width="{w}" height="{h}">\n'
            f'{rect}\n{body}\n</svg>\n')


def write_svgs():
    for key, name, _, fn in CANDIDATES:
        (SVG_DIR / f"c{key}-mark.svg").write_text(svg(fn(INK, LIME, PAPER, "full")))
        (SVG_DIR / f"c{key}-mark-dark.svg").write_text(svg(fn(PAPER if False else INK, LIME, PAPER, "dark"), bg=INK))
        (SVG_DIR / f"c{key}-mark-mono.svg").write_text(svg(fn(INK, LIME, PAPER, "mono")))
        # Adaptive-icon foreground: 108 canvas, mark inside the 66 safe zone.
        fg = f'<g transform="translate(24,24) scale(0.6)">{fn(INK, LIME, PAPER, "icon")}</g>'
        (SVG_DIR / f"c{key}-launcher-foreground.svg").write_text(svg(fg, 108, 108, "0 0 108 108"))
    # Wordmarks
    (SVG_DIR / "c1-wordmark.svg").write_text(svg(wordmark(INK, LIME, "dot"), 402, 150, "0 -45 402 150"))
    (SVG_DIR / "c1-wordmark-dark.svg").write_text(svg(wordmark(PAPER, LIME, "dot"), 402, 150, "0 -45 402 150", bg=INK))
    (SVG_DIR / "c4-wordmark.svg").write_text(svg(wordmark(INK, LIME, "acute", misreg=(6, 6)), 414, 156, "0 -46 414 156"))
    # Recommended system: cameo mark + the dot-accent wordmark.
    lock = (f'<g transform="translate(0,-10) scale(1.3)">{c2_mark(INK, LIME, PAPER, "full")}</g>'
            + wordmark(INK, LIME, "dot", scale=0.9, x0=150, y0=15))
    (SVG_DIR / "sistema-camafeo-lockup.svg").write_text(svg(lock, 520, 130, "0 -20 520 130"))
    (SVG_DIR / "c4-wordmark-dark.svg").write_text(svg(wordmark(PAPER, LIME, "acute", misreg=(6, 6)), 414, 156, "0 -46 414 156", bg=INK))


# ------------------------------------------------------------------ sheet
ICON_BG = {"1": INK, "2": LIME, "3": INK, "4": INK}  # launcher background per candidate


def icon_html(key, fn, size, shape):
    bg = ICON_BG[key]
    inner = svg(f'<g transform="translate(24,24) scale(0.6)">{fn(INK, LIME, PAPER, "icon")}</g>', 108, 108, "0 0 108 108")
    full = size * 108 / 72
    off = -size * 18 / 72
    return (f'<div class="icon {shape}" style="width:{size}px;height:{size}px;background:{bg}">'
            f'<div style="position:absolute;left:{off}px;top:{off}px;width:{full}px;height:{full}px">{inner}</div></div>')


def lockup_html(key, fn):
    """Mark + MAMÁ in Lexend (for 2 and 3); 1 and 4 show their own wordmark."""
    if key == "1":
        return svg(wordmark(INK, LIME, "dot", scale=1), 402, 150, "0 -45 402 150")
    if key == "4":
        return svg(wordmark(INK, LIME, "acute", misreg=(6, 6)), 414, 156, "0 -46 414 156")
    return (f'<div class="lockup">{svg(fn(INK, LIME, PAPER, "full"), 96, 96)}'
            f'<span class="lexend">MAM<span class="acc">Á</span></span></div>')


def lockup_dark_html(key, fn):
    if key == "1":
        return svg(wordmark(PAPER, LIME, "dot"), 402, 150, "0 -45 402 150")
    if key == "4":
        return svg(wordmark(PAPER, LIME, "acute", misreg=(6, 6)), 414, 156, "0 -46 414 156")
    return (f'<div class="lockup">{svg(fn(INK, LIME, PAPER, "dark"), 96, 96)}'
            f'<span class="lexend" style="color:{PAPER}">MAMÁ</span></div>')


def build_sheet():
    rows = []
    for key, name, desc, fn in CANDIDATES:
        icons = "".join(
            icon_html(key, fn, s, sh) for s in (108, 48) for sh in ("circle", "squircle"))
        rows.append(f"""
<section class="row">
  <div class="label"><div class="num">{key}</div><h2>{name}</h2><p>{desc}</p></div>
  <div class="cell paper"><div class="mark">{svg(fn(INK, LIME, PAPER, "full"), 150, 150)}</div>{lockup_html(key, fn)}</div>
  <div class="cell dark"><div class="mark">{svg(fn(INK, LIME, PAPER, "dark"), 150, 150)}</div>{lockup_dark_html(key, fn)}</div>
  <div class="cell paper mono"><div class="mark">{svg(fn(INK, LIME, PAPER, "mono"), 150, 150)}</div><small>Monocromo</small></div>
  <div class="cell launcher"><div class="icons">{icons}</div><small>Ícono adaptativo · 108 px y 48 px · círculo y squircle</small></div>
</section>""")
    html = f"""<!doctype html><html lang="es"><head><meta charset="utf-8">
<style>
@font-face{{font-family:Lexend;src:url("{FONT_DIR}/lexend_bold.ttf");font-weight:700}}
@font-face{{font-family:Lexend;src:url("{FONT_DIR}/lexend_medium.ttf");font-weight:500}}
@font-face{{font-family:Lexend;src:url("{FONT_DIR}/lexend_regular.ttf");font-weight:400}}
*{{box-sizing:border-box;margin:0;padding:0}}
body{{width:1800px;background:{PAPER};color:{INK};font-family:Lexend,sans-serif;padding:48px 56px 40px}}
header{{display:flex;justify-content:space-between;align-items:baseline;margin-bottom:28px}}
h1{{font-size:38px;font-weight:700;letter-spacing:-.5px}} h1 span{{color:{OLIVE};font-weight:500}}
.cols{{display:grid;grid-template-columns:300px 1fr 1fr 300px 420px;gap:16px;font-size:13px;color:#46483B;font-weight:500;padding:0 0 8px}}
.row{{display:grid;grid-template-columns:300px 1fr 1fr 300px 420px;gap:16px;margin-bottom:18px}}
.label{{padding:8px 4px}} .num{{display:inline-block;background:{LIME};color:{INK};font-weight:700;border-radius:999px;width:34px;height:34px;line-height:34px;text-align:center;margin-bottom:10px}}
.label h2{{font-size:24px;font-weight:700;margin-bottom:6px}} .label p{{font-size:14px;line-height:1.4;color:#46483B}}
.cell{{border-radius:22px;padding:22px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:16px;min-height:290px;position:relative}}
.paper{{background:#fff;box-shadow:0 1px 0 rgba(0,0,0,.04),0 10px 30px rgba(24,25,24,.07)}}
.dark{{background:{INK};color:{PAPER}}}
.mark svg{{display:block}}
.lockup{{display:flex;align-items:center;gap:14px}} .lexend{{font-weight:700;font-size:56px;letter-spacing:.04em}}
.cell > svg{{width:300px;height:auto}}
small{{position:absolute;bottom:10px;font-size:12px;color:#46483B;font-weight:500}}
.launcher{{background:#E9EAE2;}} .icons{{display:flex;align-items:center;gap:22px}}
.icon{{position:relative;overflow:hidden;flex:none}} .icon svg{{display:block;width:100%;height:100%}}
.circle{{border-radius:50%}} .squircle{{clip-path:path("M0.5,0 C0.1,0 0,0.1 0,0.5 0,0.9 0.1,1 0.5,1 0.9,1 1,0.9 1,0.5 1,0.1 0.9,0 0.5,0 Z");clip-path:url(#sq)}}
footer{{margin-top:14px;font-size:13px;color:#46483B}}
</style></head><body>
<svg width="0" height="0" style="position:absolute"><defs><clipPath id="sq" clipPathUnits="objectBoundingBox"><path d="M0.5,0 C0.12,0 0,0.12 0,0.5 0,0.88 0.12,1 0.5,1 0.88,1 1,0.88 1,0.5 1,0.12 0.88,0 0.5,0 Z"/></clipPath></defs></svg>
<header><h1>MAMÁ <span>· candidatos de logo e ícono</span></h1>
<div style="font-size:14px;color:#46483B">Carbón {INK} · Chartreuse {LIME} · Papel {PAPER} · Oliva {OLIVE}</div></header>
<div class="cols"><div></div><div>Color, fondo claro</div><div>Modo oscuro</div><div>Monocromo</div><div>Ícono de la app (Android)</div></div>
{''.join(rows)}
<footer>Todo es SVG hecho a mano (archivos en logo/svg/). El ícono adaptativo muestra la zona segura de 66 dp sobre un lienzo de 108 dp, como lo recorta Android.</footer>
</body></html>"""
    (HERE / "sheet.html").write_text(html)


if __name__ == "__main__":
    write_svgs()
    build_sheet()
    print("ok", sorted(p.name for p in SVG_DIR.iterdir()))
