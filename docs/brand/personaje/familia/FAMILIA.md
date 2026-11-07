# La familia de Mamá

Siete personajes en el mismo estilo riso de Mamá, pensados para que después puedan ser los avatares de "tu familia" en la app (los contactos que reciben el aviso), piezas del pitch o personajes de otros flujos (la tía Carmen es la de la frase clave).

Carpetas:
- `raw/`: lo que salga de Gemini, un PNG por personaje, con el nombre de la tabla.
- `export/`: los WebP recortados y alineados (los hago yo con `tools/slice_sheet.py` o `tools/cutout.py`).

## El elenco

| Archivo | Quién es | Rol en la historia |
|---|---|---|
| `mama` | Mamá (ya existe: `../mama-master.png`) | La que llama |
| `papa` | Papá, el esposo | El tranquilo, "yo resuelvo" |
| `tia_carmen` | La hermana de Mamá | La de la frase clave; se entera de todo primero |
| `hermana` | La hija mayor, veintipico | La que contesta el WhatsApp primero |
| `hermano` | El hijo, veintipico | El relajado, siempre con el teléfono |
| `tio` | El hermano de Papá | El parrandero |
| `abuela` | La mamá de Mamá | El centro de la familia |
| `abuelo` (opcional) | El esposo de la abuela | El del sombrero |

## Reglas para que parezcan de la misma familia

- **Misma construcción que Mamá:** busto en 3/4 dentro de un círculo chartreuse, piel de papel sin tinta, pelo y rasgos en carbón, ropa en oliva con detalles chartreuse. Sin colores nuevos.
- **Mismo tamaño de cabeza respecto al círculo.** Si uno sale más chico, se regenera.
- **Cada uno tiene un objeto que lo identifica** (ver fichas). Es lo que los distingue a 48 px: los lentes en la cabeza son de Mamá; nadie más los lleva así.
- **Generar de uno en uno, pasando siempre `mama-master.png` como referencia de estilo.** Nunca desde cero. Y en la misma conversación de Gemini, para que arrastre el estilo.
- Después de tener a todos, la foto familiar se genera pasándole las imágenes individuales.

## Bloque de estilo (se pega antes de cada personaje)

```
Use the attached illustration of "Mamá" ONLY as the style and construction reference:
same risograph print technique, same flat hand-cut shapes with slightly wobbly edges,
same four-ink palette (charcoal #181918, fluorescent chartreuse #EFFE00, olive #4A5400,
and the off-white paper #FBFBF2 left unprinted for the skin), same heavy grain, slight
off-register shift and overprint, same 3/4 head-and-shoulders framing inside ONE
chartreuse circle on off-white paper, same head-to-circle scale.
Design a NEW member of the same family, drawn by the same illustrator for the same
series. Do not copy Mamá's face, hair or accessories. Square 1:1. No text.

Character:
```

Y al final de cada uno:

```
Avoid: realistic, 3D, gradients, shading, any color outside the four inks, caricature,
exaggerated features, stereotype, text, logo, watermark, sunglasses on top of the head
(that is Mamá's), copying the reference face.
```

## Fichas y prompts

### Papá · `papa`
Señor venezolano de casi 60, tranquilo y sólido. Objeto: los lentes de leer colgando del cuello y el cafecito.

```
"Papá", a Venezuelan dad in his late 50s, calm and solid, the one who says "tranquilo,
yo resuelvo". Short charcoal hair receding at the temples with paper-colored gray
streaks at the sides, a thick well-kept mustache with a few gray hairs, a soft double
chin, kind half-closed eyes, relaxed brows, a small content smile. Reading glasses
hanging from the neckline of his shirt. Olive guayabera with thin chartreuse pinstripes
and two chest pockets, a chartreuse wristwatch, holding a tiny cafecito cup near the
bottom edge of the circle.
```

### Tía Carmen · `tia_carmen`
La hermana de Mamá, la que se entera de todo primero. Objeto: los rulos chartreuse y la mano al lado de la boca contando un secreto.

```
"Tía Carmen", Mamá's sister, early 50s, the loving gossip aunt who always knows first.
Hair in big chartreuse hair rollers under a sheer paper-colored scarf knotted at the
front, cat-eye glasses worn ON her face (charcoal frame), dark charcoal lips slightly
open as if telling a secret, one hand raised to the side of her mouth, mischievous
raised eyebrows. Olive robe with chartreuse polka dots, big chartreuse hoop earrings,
a coffee mug at the bottom edge.
```

### La hermana · `hermana`
Veintipico, universitaria, lista y leal. Objeto: el moño alto y los audífonos inalámbricos chartreuse.

```
"La hermana", early 20s, a university student, sharp and loyal, the one who answers
the family chat first. High sleek charcoal bun with styled baby hairs, big chartreuse
hoop earrings, chartreuse wireless earbuds, thin winged eyeliner, a tiny chartreuse
nose stud, an oversized olive hoodie, holding a phone with a chartreuse case, a
knowing "ajá, cuéntame" look with one eyebrow up.
```

### El hermano · `hermano`
Veintipico, relajado. Objeto: la gorra carbón con visera chartreuse.

```
"El hermano", mid 20s, easygoing and warm. Charcoal baseball cap worn slightly back
with a chartreuse brim and a small chartreuse star, short curly charcoal hair peeking
out, a short beard, thick brows, a wide grin with one dimple, an olive crew-neck tee,
a thin chartreuse chain, one earbud in.
```

### El tío · `tio`
El hermano de Papá, el parrandero. Objeto: los lentes de aviador puestos y el cuatro.

```
"El tío", Papá's younger brother, mid 50s, the party uncle. Aviator sunglasses worn
ON his face with chartreuse lenses and a thin paper-colored frame, a thick mustache,
slicked-back charcoal hair with one gray streak, a big open laugh showing teeth, an
olive short-sleeve shirt with a bold chartreuse hibiscus print open at the collar,
a chartreuse chain, the neck of a small cuatro (Venezuelan four-string guitar) peeking
in at the bottom edge.
```

### La abuela · `abuela`
La mamá de Mamá, el centro de la familia. Objeto: el moño blanco y las perlas.

```
"La abuela", late 70s, Mamá's mother, the center of the family, patient and loving.
Paper-colored white hair in a neat low bun, round charcoal glasses, a few thin shaky
lines for soft wrinkles, a gentle closed-mouth smile, warm eyes. A paper-colored pearl
necklace over an olive cardigan with a chartreuse brooch, small chartreuse clip-on
earrings, hands folded over a small handkerchief at the bottom edge.
```

### El abuelo (opcional) · `abuelo`
Objeto: el sombrero de paja.

```
"El abuelo", early 80s, quiet and amused. A paper-colored straw hat with a charcoal
band, a white (paper-colored) short mustache and white hair at the temples, deep
smile lines, a paper-colored short-sleeve shirt with charcoal suspenders, a chartreuse
pocket square, the curved handle of a wooden cane at the bottom edge.
```

## Juntos

**La pareja** (pasar `mama-master.png` y `raw/papa.png`):
```
Keep both attached characters exactly as they are (same faces, hair, accessories,
clothes, inks, grain). Draw them together inside ONE larger chartreuse circle: Mamá
on the left with her head slightly leaning toward Papá's shoulder, Papá on the right
with his arm around her, both looking at the viewer with small smiles. Same paper
background, same style. Square 1:1. No text.
```

**La foto familiar** (pasar todas las imágenes):
```
Keep every attached character exactly as they are. Arrange them as a family portrait
in two rows inside ONE wide chartreuse oval on off-white paper: grandparents seated in
front at the center, Mamá and Papá standing behind them, Tía Carmen and el tío at the
sides, la hermana and el hermano at the ends, slightly overlapping like a real family
photo. Same risograph style, inks and grain. Landscape 16:9. No text.
```

## Cuando tengas las imágenes

1. Guárdalas en `raw/` con el nombre de la tabla (`papa.png`, `tia_carmen.png`...).
2. Me avisas y las recorto, las alineo al mismo círculo que Mamá y las exporto a `export/` en WebP de 512 px, listas para la app.
