# Mamá: ficha del personaje

La imagen maestra es `mama-master.png`. Todas las poses se generan a partir de ella, nunca desde cero.

## Rasgos fijos (no cambian entre poses)

- **Fondo:** un círculo chartreuse `#EFFE00` con grano, detrás de la cabeza y los hombros. El papel es `#FBFBF2`.
- **Pelo:** un blower carbón `#181918` voluminoso, con las puntas hacia afuera y vetas grises de pincel.
- **Lentes:** de sol, tipo cat-eye, subidos en la cabeza. El marco es color papel y los cristales chartreuse.
- **Cara:** piel color papel. Cejas arqueadas (la derecha levantada) y ojos con 3 pestañas en ala. Mira arriba a la derecha. Nariz de una sola curva, labios carbón y media sonrisa pícara.
- **Joyas:** argollas chartreuse en las dos orejas, y una cadenita carbón fina con una medalla redonda chartreuse.
- **Blusa:** oliva `#4A5400` con lunares chartreuse y escote en V. Es una cúpula que sale por debajo del círculo.
- **Técnica:** riso con grano fuerte, densidad de tinta dispareja y un leve desfase entre tintas. Sin degradados ni 3D.
- **Encuadre:** busto centrado en un formato cuadrado 1:1, y el mismo tamaño de círculo en todas las poses.

## Prompt base para cualquier pose

Pásale `mama-master.png` y escribe:

```
Keep this exact character and illustration identical: same chartreuse circle (same size
and position), same paper background, same charcoal blowout hair shape, same paper-colored
cat-eye sunglasses with chartreuse lenses on her head, same face drawing style (arched
brows, winged lashes, dark charcoal lips), same chartreuse hoop earrings and medal
necklace, same olive polka-dot V-neck blouse, same risograph grain, off-register and ink
texture, same framing and scale. Square 1:1. No text.
Change only:
```

## Estados

| Archivo | Estado | Qué escribir después de "Change only:" | Dónde se usa |
|---|---|---|---|
| `mama_pendiente` | Pendiente | (la imagen maestra tal cual) | Configuración e inicio |
| `mama_parpadeo` | Parpadeo | `her eyes are gently closed (curved lids with the same winged lashes), everything else identical` | Animación de parpadeo |
| `mama_despierta` | Despierta | `eyebrows raised high, eyes wide open looking straight at the viewer, lips slightly parted in surprise` | Se detectó el gesto |
| `mama_llamando` | Llamando | `she holds a small charcoal smartphone to her ear with one paper-colored hand entering from the bottom, determined smile, looking at the viewer` | Twilio marcando |
| `mama_escuchando` | Escuchando | `head tilted slightly toward her shoulder, eyes looking to the side, attentive serious expression, lips closed` | Llamada en curso |
| `mama_escribiendo` | Escribiendo | `looking down at a small charcoal smartphone held in front of her chest with both paper-colored hands, focused little smile` | Aviso enviándose (solo en pantallas seguras) |
| `mama_lista` | Lista | `eyes closed in a relieved warm smile, one paper-colored hand on her chest` | Todo listo |

Hoja de expresiones, útil para revisar que el personaje se mantiene igual:

```
Using the attached character exactly as she is, create an expression sheet: a 3 by 2 grid
of head-and-shoulders close-ups on the same paper background, each with its own small
chartreuse circle: warm smirk, raised eyebrow "really?", surprised, listening, focused,
relieved with eyes closed. Same drawing style, grain and colors. No text, no labels.
```

## Exportar

1. Recortar el fondo de papel con `python3 tools/cutout.py entrada.png salida.png preview.png`, para que quede transparente y sirva en modo oscuro.
2. Pasar a WebP de 512 px (unos 65 KB cada uno) y ponerlos en `export/` con el nombre de la tabla.
3. En la app van en `app/src/main/res/drawable-nodpi/`.
