# Aló Mamá brand files

Ready-to-share exports of the Aló Mamá logo. The app's own launcher icon and `MamaMark` live under `app/src/main/res/` and are not built from these files.

| File | What it is | Use |
| --- | --- | --- |
| `alo-mama-logo-cuadrado.png` | 1024 px, full-bleed chartreuse | App icon, store listing |
| `alo-mama-logo-redondo.png` | 1024 px, round, transparent corners | Profile pictures |
| `alo-mama-lockup-claro.png` | Logo + "ALÓ MAMÁ" + slogan on paper `#FBFBF2`, 3200×1600 | Slides, light backgrounds |
| `alo-mama-lockup-oscuro.png` | Same on charcoal `#181918` | Slides, dark backgrounds |
| `alo-mama-ilustracion.png` | The original riso illustration with its circle | Print, pitch |
| `alo-mama-ilustracion-sin-fondo.png` | The illustration on a transparent background | Stickers, compositions |

Name: the product is "Aló Mamá"; the wordmark spells it "ALÓ MAMÁ". "Mamá" alone is the character, the contact that calls. Slogan: "Mamá, siempre en el momento correcto". Palette: charcoal `#181918`, chartreuse `#EFFE00`, paper `#FBFBF2`, olive `#4A5400`. Wordmark font: Lexend 700.

## Character and logo sources (`personaje/`)

Everything the exports above, the app's Mamá poses and the launcher icon were made from. Nothing in the app or server reads these files.

| Path | What it is |
| --- | --- |
| `personaje/PERSONAJE.md` | Character bible: fixed traits, palette, how each pose is generated from the master image |
| `personaje/mama-master.png`, `mama-cutout.png` | The master illustration and its cutout (same files as `alo-mama-ilustracion*.png`) |
| `personaje/mama-options.png` | The first generation sheet the master was picked from |
| `personaje/export/` | The pose WebPs; `export/app/` is what went into `app/src/main/res/drawable-nodpi/` |
| `personaje/logo/` | Round 1 logo candidates (`svg/`, `android/`, `sheet.png`, built by `build_sheet.py`) and round 2 (`v2/`), the one the app ships. `logo/README.md` tells the story |
| `personaje/familia/` | Brief and mock for Mamá's family as future avatars (not in the app) |
| `personaje/screens/` | Permission-screen mockups used while designing setup |
| `personaje/runway-inputs/` | Pose frames sized for Runway; Runway was never used, the talking video is local |
| `personaje/tools/` | Local scripts: `slice_sheet.py` and `cutout.py` cut poses, `riso_post.sh` adds the riso grain, `mama_habla.sh` + `track_face.py` + `mama_puppet.py` make the "Mamá hablando" puppet video from a driving video |
| `personaje/video/` | The puppet videos (Mamá alone, square and vertical) and the side-by-side with the driving video |

The driving video itself (a WhatsApp recording) is not in the repo; `tools/mama_habla.sh` takes it as an argument.
