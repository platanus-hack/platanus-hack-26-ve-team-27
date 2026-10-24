# MAMÁ

**Pide ayuda en secreto: una llamada de "Mamá" que, si estás en peligro, avisa a tu familia por WhatsApp.**

En Venezuela la seguridad personal es una preocupación de todos los días. Cuando alguien está en peligro (un secuestro, un robo) sacar el teléfono para pedir ayuda puede empeorar las cosas. MAMÁ convierte una llamada normal con tu mamá en una alarma silenciosa.

## Cómo funciona

1. **Configuración.** Pones tu nombre, los números de tu familia y dos frases: la de alerta (*"dile a la tía Carmen que llego tarde"*) y la de todo bien (*"voy llegando, ya casi"*). La app guarda a "Mamá" en tus contactos.
2. **Disparo discreto.** Presionas volumen **arriba-abajo-arriba**, aunque el teléfono esté bloqueado. La app envía tu ubicación GPS al servidor.
3. **Te llama "Mamá".** En menos de 15 segundos te entra una llamada normal de "Mamá", un agente de voz con IA (ElevenLabs + Twilio) que te saluda por tu nombre y te pregunta cómo vas.
4. **Detector de coacción.** Reglas + un LLM revisan la llamada. Si dices la frase de alerta o te quedas callado, estás en peligro. Si dices la frase de todo bien, no se avisa a nadie. Si no dices ninguna de las dos, se avisa igual: no confirmaste que estás bien.
5. **Aviso a la familia.** Tu familia recibe un WhatsApp (Kapso) con tu ubicación, el veredicto, lo que se entendió de la llamada (dónde estás, con quién, qué parece estar pasando) y el enlace a la ficha del caso, con la grabación de la llamada cuando ya está lista.
6. **Central.** Una central web privada muestra cada alerta en vivo, con su estado y tu recorrido, y su ficha completa cuando termina la llamada.

Para quien te esté mirando, es solo una llamada con tu mamá. Si usas el widget de la pantalla de inicio, MAMÁ no llama: avisa a tu familia de inmediato.

## Stack

- **App:** Android nativo en Kotlin (Jetpack Compose + Material 3).
- **Servidor:** Go + Postgres en Render.
- **Voz y llamadas:** ElevenLabs Agents + Twilio.
- **Mensajería:** Kapso (WhatsApp).
- **Evaluación:** 70 diálogos etiquetados para medir precisión y recall del detector de coacción, con y sin frase de todo bien.

## Equipo

Samuel, Daniel Ross, Sebas, Daniel Castellanos y Mariana — Platanus Hack 26 Caracas, equipo 27.
