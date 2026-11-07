# Logo e ícono de MAMÁ: candidatos

Hoja comparativa: `sheet.png` (se regenera con `python3 build_sheet.py && ./render.sh`).
Todo es SVG hecho a mano en `svg/`; no hay tipografía externa salvo Lexend en los lockups de 2 y 3, que es la fuente de la app.

Colores: carbón `#181918`, chartreuse `#EFFE00`, papel `#FBFBF2`, oliva `#4A5400` (primario en modo claro).
Regla que cumplen los cuatro: tienen que verse como una app de familia, nunca como un botón de pánico. Sin escudos, sirenas, rojo ni signos de exclamación.

## Los cuatro candidatos

| # | Nombre | Qué significa | Archivos |
|---|---|---|---|
| 1 | **Á con halo** | El wordmark MAMÁ con letras geométricas propias; el acento de la Á es un círculo chartreuse: el halo de Mamá, su argolla. La marca sola es la Á. | `c1-wordmark*.svg`, `c1-mark*.svg`, `c1-launcher-foreground.svg` |
| 2 | **Camafeo** | Mamá de perfil, como el camafeo de una señora: el blower con volumen, los lentes cat-eye apoyados en la cabeza y la argolla. Una sola forma carbón dentro del círculo chartreuse. | `c2-mark*.svg`, `c2-launcher-foreground.svg`, `sistema-camafeo-lockup.svg` |
| 3 | **Llamada** | Una burbuja de llamada cuyo único contenido es una sonrisa: la llamada de Mamá que te tranquiliza. | `c3-mark*.svg`, `c3-launcher-foreground.svg` |
| 4 | **Riso M** | Una M impresa dos veces, con la pasada chartreuse fuera de registro, como la ilustración riso del personaje. | `c4-mark*.svg`, `c4-wordmark*.svg`, `c4-launcher-foreground.svg` |

Cada candidato tiene versión a color sobre papel, modo oscuro, monocromo y vista previa del ícono adaptativo de Android (lienzo de 108 dp, zona segura de 66 dp, máscaras círculo y squircle, a 108 px y a 48 px).

## Recomendación: el camafeo (2) como marca e ícono, con el wordmark de la Á con halo (1)

El ícono tiene que sobrevivir a 48 px entre otros treinta íconos y decir "esto es una app cálida de familia" antes de que nadie lea el nombre. El camafeo es el único de los cuatro que hace las dos cosas: a 48 px se sigue leyendo como una mujer de perfil con una argolla, y es el único que nadie más tiene; una silueta de señora arreglada es inconfundible en una pantalla llena de burbujas de chat y letras iniciales. Además conecta directo con el personaje que ya está en las pantallas y en el pitch (mismo blower, mismos lentes, misma argolla, mismo círculo chartreuse), así que el ícono y la ilustración se refuerzan en vez de competir. La burbuja con sonrisa (3) es la más limpia y la más segura, pero se parece a cualquier app de mensajería y no cuenta nada de Mamá; queda como plan B si al equipo el camafeo le parece anticuado. La Á con halo (1) es un wordmark fuerte pero una letra sola es un ícono débil, y el punto encima de la A se puede leer como una Å nórdica; por eso la propongo solo como wordmark, acompañando al camafeo: el punto repite el círculo de la marca. La Riso M (4) es la más estilosa de cerca y la más floja de lejos: una M sola es ambigua (McDonald's, Messenger, Gmail) y la desregistración se vuelve una sombra a 48 px. El lockup del sistema recomendado está en `svg/sistema-camafeo-lockup.svg`.

Descarté por el camino una silueta frontal (pelo + cara + lentes de frente): a 48 px se leía como un búho, y con argollas, como una criatura con ojos a los lados.

## Qué reemplazaría en la app (todavía no aplicado)

Los vectores del camafeo ya están escritos en `android/`, con los mismos nombres que usa el repo:

| Archivo en `logo/android/` | Reemplaza a | Nota |
|---|---|---|
| `ic_launcher_foreground.xml` | `app/src/main/res/drawable/ic_launcher_foreground.xml` | Perfil carbón, lente chartreuse con marco papel y argolla chartreuse, dentro de la zona segura de 66 dp. |
| `ic_launcher_background.xml` | `app/src/main/res/drawable/ic_launcher_background.xml` | Pasa de carbón a chartreuse: el ícono es el círculo de Mamá. |
| `ic_launcher_monochrome.xml` | (nuevo) | Capa `<monochrome>` para íconos temáticos; hoy el manifiesto reutiliza el foreground, y habría que apuntar `mipmap-anydpi-v26/ic_launcher*.xml` a este archivo. El lente y la argolla son huecos (even-odd), así que sobreviven a cualquier tinte. |
| `ic_mama_mark.xml` | `app/src/main/res/drawable/ic_mama_mark.xml` | La marca en un solo color para `MamaMark()`; viewport 100 en vez de 24. |

Al aplicarlo, `MamaMark()` en `Theme.kt` debería pasar de la baldosa `shapes.small` a `CircleShape` con `primaryContainer` (chartreuse) de fondo y el perfil en `onPrimaryContainer`, para que se parezca al ícono. Eso toca `Theme.kt`, que es del ticket #5, y el ícono del launcher no puede leer el tema, así que los hex quedan duplicados a propósito (igual que hoy).

## No verificado

- Los XML de `android/` no se compilaron ni se instalaron: la geometría es la misma que la de los SVG (que sí se renderizaron), pero no probé el ícono en un launcher real ni la capa monocromo con íconos temáticos.
- Las letras de los wordmarks 1 y 4 son geométricas propias, no Lexend; si el equipo prefiere Lexend, el lockup de 2 y 3 ya lo muestra con la fuente de la app.

---

# Ronda 2: la marca es ella (`v2/`)

Daniel descartó la ronda 1: el camafeo era una silueta genérica y no se parecía a la Mamá que generamos. En esta ronda todo parte de la ilustración misma. Hoja comparativa: `v2/sheet.png` (se regenera con Chrome headless: `cd v2 && "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=2 --window-size=1800,2460 --virtual-time-budget=4000 --screenshot="$PWD/sheet.png" "file://$PWD/sheet.html"`).

| # | Candidato | Qué es | Archivos |
|---|---|---|---|
| 1 | **Ilustración directa** | El dibujo riso tal cual, sin su círculo (el fondo del ícono ya es chartreuse), encuadrado para que el pelo ocupe 74 dp del lienzo de 108. Dos variantes: grano original y grano suavizado (mediana de 7 px sobre el máster). | `v2/raster/build_raster.py`, `v2/raster/mama-knockout.png`, `v2/raster/icon_*`, `v2/raster/play_*_512.png` |
| 2 | **Sticker vectorial** | Mamá calcada sobre el máster (mismas coordenadas, `v2/trace/trace.html` superpone el SVG al dibujo), en tintas planas y sin grano. Tiene versión redonda, modo oscuro y monocroma. | `v2/svg/sticker-full*.svg`, `v2/svg/mark.svg`, `v2/svg/mark-mono.svg` |
| 3 | **Sticker mínimo** | La misma silueta con menos rasgos. A: sin pestañas ni cadena y trazos más gruesos. B: solo lentes, argollas y labios. | `v2/svg/sticker-minA*.svg`, `v2/svg/sticker-minB*.svg`, `v2/svg/mark-min*.svg` |
| 4 | **Lockup** | MAMÁ en Lexend 700 con ella: (a) su cabeza como acento de la Á, (b) marca + wordmark, (c) wordmark bajo el círculo. | compuestos en `v2/sheet.html` |

## Recomendación: ícono = la ilustración (1, suavizada); marca = el sticker (2)

Para el ícono del launcher la ilustración gana. A 108 px es inconfundiblemente nuestra Mamá, con su expresión, y a 48 px se lee igual o mejor que el sticker: la cara de cualquier personaje se vuelve mancha a ese tamaño, pero las proporciones reales del dibujo (la masa de pelo, los lentes claros arriba, la cara clara, la blusa oscura abajo) sobreviven mejor que un calco de trazos, cuyos rasgos se convierten en ruido gris. Entre las dos variantes, la suavizada: por debajo de 108 px el grano riso ya no se ve como grano sino como suciedad, y la mediana lo quita sin tocar los bordes; a 324 y 432 px la diferencia con el original es imperceptible. El sticker vectorial es la **marca**: escala a cualquier tamaño para el pitch, los materiales impresos y el `MamaMark()` de la app, y es el único que da versión monocroma y la capa de ícono temático de Android (que exige un solo color). Como lockup principal va (b), marca a la izquierda y wordmark en Lexend; (a), ella como acento de la Á, es la versión juguetona para el splash y la portada del pitch. El sticker mínimo B se descarta: sin ojos ni cejas la cara en blanco inquieta a tamaño grande, y a 48 px no gana lo suficiente para justificarlo; A no mejora lo bastante sobre el completo.

## Qué reemplaza en la app (todavía no aplicado)

| Archivo en `logo/v2/android/` | Reemplaza a | Nota |
|---|---|---|
| `mipmap-{m,h,x,xx,xxx}hdpi/ic_launcher_foreground.png` | `res/drawable/ic_launcher_foreground.xml` (vector) | Se convierte en recurso `@mipmap/ic_launcher_foreground` bitmap, uno por densidad (108/162/216/324/432 px). Variante suavizada. |
| `ic_launcher_background.xml` | `res/drawable/ic_launcher_background.xml` | Chartreuse plano: el ícono es el círculo de Mamá. |
| `ic_launcher_monochrome.xml` | (nuevo) `res/drawable/` | Capa `<monochrome>` para íconos temáticos: pelo con la cara, los lentes y las argollas como huecos (even-odd) más los labios. |
| `mipmap-anydpi-v26/ic_launcher.xml`, `ic_launcher_round.xml` | los mismos en `res/mipmap-anydpi-v26/` | Apuntan al foreground bitmap, al fondo y a la capa monocroma. |
| `ic_launcher-playstore.png` | (nuevo, raíz de `app/`) | 512 px sin máscara, por si se publica. |
| `ic_mama_mark.xml` | `res/drawable/ic_mama_mark.xml` | Marca de un solo color (anillo + silueta) para `MamaMark()`, viewport 100. `MamaMark()` debería pasar a `CircleShape` sobre `primaryContainer`. |
| `vector/ic_launcher_foreground.xml` | alternativa vectorial al PNG | El sticker completo como foreground, por si el equipo prefiere vector; pesa 42 paths. |

## Regenerar vectores

Los SVG de `v2/svg/` son la fuente. Los XML se generaron con un conversor mínimo (nuestro subconjunto de SVG: `path`, `circle`, `g` con `fill`/`stroke`/`stroke-width`/`stroke-linecap`/`stroke-linejoin`): cada `circle` pasa a dos arcos, el grupo aplica `scale = 74/1044` y `translate = (54 − 632·k, 52 − 517·k)` para el lienzo de 108 dp, o `scale = 100/1134`, `translate = (−61·s, −53·s)` para el viewport 100 de la marca. Si cambias un SVG, vuelve a aplicar esa transformación.

## No verificado en la ronda 2

- Los PNG y XML de `v2/android/` no se compilaron ni se instalaron en un launcher; la geometría es la de los SVG que sí se renderizaron, y los XML son XML bien formado. La capa monocroma se probó solo como SVG teñido en el navegador.
- El calco vectorial se ajustó a ojo sobre el máster (tres iteraciones con la superposición); no es un trazado automático.
