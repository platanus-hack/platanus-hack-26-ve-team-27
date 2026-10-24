# MAMÁ

**Pide ayuda en secreto: una llamada de "Mamá" que, si estás en peligro, avisa a tu familia por WhatsApp.**

En Venezuela la seguridad personal es una preocupación de todos los días. Cuando alguien está en peligro (un secuestro, un robo) sacar el teléfono para pedir ayuda puede empeorar las cosas. MAMÁ convierte una llamada normal con tu mamá en una alarma silenciosa.

## Cómo funciona

1. **Disparo discreto.** Presionas volumen **arriba-abajo-arriba**, o tocas el widget de la pantalla de inicio.
2. **Captura.** La app envía tu ubicación GPS y velocidad al servidor.
3. **Te llama "Mamá".** En menos de 15 segundos recibes una llamada de un agente de voz con IA (ElevenLabs + Twilio).
4. **Detector de coacción.** Si dices la frase acordada (*"dile a la tía Carmen que llego tarde"*) o te quedas en silencio, reglas + un LLM detectan que estás en peligro.
5. **Aviso a la familia.** Tu familia recibe al instante un WhatsApp (Kapso) con tu ubicación y el veredicto.

Para quien te esté mirando, es solo una llamada con tu mamá. Si vas en un vehículo a alta velocidad, MAMÁ no llama: avisa a tu familia de inmediato y sigue enviando tu ubicación en silencio.

## Stack

- **App:** Android nativo en Kotlin (Jetpack Compose + Material 3).
- **Servidor:** Go + Postgres en Render.
- **Voz y llamadas:** ElevenLabs Agents + Twilio.
- **Mensajería:** Kapso (WhatsApp).
- **Evaluación:** más de 50 diálogos etiquetados para medir precisión y recall del detector de coacción.

## Equipo

Samuel, Daniel Ross, Sebas, Daniel Castellanos y Mariana — Platanus Hack 26 Caracas, equipo 27.
