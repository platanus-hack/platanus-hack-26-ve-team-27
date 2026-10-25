# Guion de la demo offline (#61)

Cómo mostrar que una emergencia llega sin internet, por SMS y por la malla BLE, y cómo forzar el canal para que la demo no dependa del wifi ni de la señal del evento. Especificación: #62 §9.

## Preparación

**Servidor** (`cd server && go run ./cmd/server`, o el deploy), con:

- `DATABASE_URL`
- `EMERGENCY_OFFLINE_ENABLED=true` y `EMERGENCY_SMS_NUMBER` (E.164, el número central de Twilio)
- `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER` y `TWILIO_WEBHOOK_BASE_URL` (URL pública, para `POST /sms/inbound/twilio`)
- `INCIDENT_REPORT_ENABLED=true` y `CENTRAL_KEY` para el dashboard de la central
- Las llaves del servidor fijas, no efímeras: si el servidor arranca con llaves efímeras, la llave que el teléfono guardó deja de servir al reiniciarlo.

**Teléfonos** (2, con MAMÁ debug y el flag offline):

```bash
MAMA_OFFLINE_EMERGENCY=1 ./gradlew assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

- Hacer el onboarding de los dos con internet: registra la llave y baja `GET /config`.
- **Emisor (A):** permisos de Bluetooth, ubicación y SMS concedidos, y SIM con señal.
- **Relé (B):** con internet y el relé activado en ajustes ("MAMÁ está atenta a emergencias cercanas").
- Abrir la central en el navegador: `<servidor>/central?key=<CENTRAL_KEY>`.

## Forzar el canal (toggle debug)

Solo en el build debug, desde adb sobre el emisor:

```bash
adb shell am broadcast -n com.team27.mama/com.team27.mama.debug.MeshDebugReceiver \
  -a com.team27.mama.debug.CHANNEL_OVERRIDE --es mode <MODO>
```

| `mode` | Qué hace |
|---|---|
| `AUTO` | Normal: alerta HTTP y, si falla, SMS y malla en paralelo |
| `NO_INTERNET` | Se salta HTTP: SMS y malla en paralelo |
| `SMS_ONLY` | Solo SMS |
| `BLE_ONLY` | Solo malla |

Si el modo no es `AUTO`, el botón de pánico también se salta la alerta online, así que va directo al canal forzado aunque el teléfono tenga internet. El override vive en memoria: al reiniciar la app vuelve a `AUTO`. `adb logcat -s MeshDebug MamaEmergency` muestra el override y la fase.

## Guion (unos 2 minutos)

1. **Contexto (15 s).** "¿Qué pasa si la víctima no tiene datos?" Mostrar la central vacía.
2. **SMS (30 s).** En A: `mode SMS_ONLY`, activar la emergencia. La central muestra el reporte con canal **SMS**. A recibe el SMS de ack y deja de reintentar. Los contactos reciben el WhatsApp degradado ("enviado sin internet").
3. **Malla (45 s).** A en **modo avión real con Bluetooth encendido** (`mode BLE_ONLY` como respaldo). Activar la emergencia. B la recibe por BLE y la sube: la central muestra canal **malla (1 salto)**. B no ve la ubicación, solo "reenviaste N alertas". El ack vuelve por la malla y A pasa a confirmado.
4. **Cancelar (15 s).** Cancelar en A (falsa alarma): el detalle del reporte en la central muestra "Emergencia sin conexión cancelada".
5. **Cierre (15 s).** La firma Ed25519 y el sealed box hacen que un relé no pueda leer ni falsificar la alerta.

Antes de cada paso, cancelar la emergencia anterior: solo hay una activa a la vez.

## Ensayo de punta a punta

- [ ] `SMS_ONLY`: la central muestra SMS y llega el ack por SMS.
- [ ] `BLE_ONLY` / modo avión: la central muestra malla con saltos y llega el ack por la malla.
- [ ] `NO_INTERNET`: la central muestra "SMS y malla".
- [ ] Cancelar se refleja en la central.
- [ ] Llega el WhatsApp al contacto.
