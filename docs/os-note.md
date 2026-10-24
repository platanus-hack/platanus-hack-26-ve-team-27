# OS note: what the phone OS lets MAMÁ do

Checked against official docs on Oct 24, 2026. **Viable**: buildable this weekend on the sideloaded demo Xiaomi. **Roadmap**: goes on Showtime's roadmap slide.

| Idea | Verdict | Why, in one line | Source |
| --- | --- | --- | --- |
| Google Emergency SOS | Roadmap | It's a system feature (power button 5 times) with no documented hook for third-party apps, and it "doesn't work in airplane mode". | [Android Help](https://support.google.com/android/answer/9319337) |
| Blocking shutdown | Roadmap | Hiding the power menu needs lock task mode, which only an app allowlisted by a device policy controller (device owner) gets. | [Lock task mode](https://developer.android.com/work/dpc/dedicated-devices/lock-task-mode) |
| Detect shutdown (extra:2) | Viable, best effort | `ACTION_SHUTDOWN` reaches only receivers registered at runtime (Android 9+), so the alert's foreground service listens. Nothing promises time to send, so the heartbeat is the real signal. | [Intent](https://developer.android.com/reference/android/content/Intent#ACTION_SHUTDOWN), [exceptions](https://developer.android.com/develop/background-work/background-tasks/broadcasts/broadcast-exceptions) |
| Detect airplane mode (extra:2) | Viable, best effort | `ACTION_AIRPLANE_MODE_CHANGED` isn't exempt for manifest receivers, so the service registers it at runtime. The radios may already be off, so the last location can fail and the heartbeat has to cover it. | [Intent](https://developer.android.com/reference/android/content/Intent#ACTION_AIRPLANE_MODE_CHANGED), [exceptions](https://developer.android.com/develop/background-work/background-tasks/broadcasts/broadcast-exceptions) |
| Heartbeat escalation (extra:2) | Viable | During an alert, a `location` foreground service posts GPS. The server flags silence. Starting the service from the background needs `ACCESS_BACKGROUND_LOCATION`. | [FGS types](https://developer.android.com/develop/background-work/services/fgs/service-types) |
| Offline SMS location (extra:1) | Viable | `SmsManager` sends with the `SEND_SMS` permission. A compact line (`M1 <id> <lat>,<lon> <acc> <unix>`) fits one SMS, and `divideMessage` handles overflow. | [SmsManager](https://developer.android.com/reference/android/telephony/SmsManager) |
| `SEND_SMS` on Google Play | Roadmap | Play restricts it to default SMS handlers, plus an "emergency alerts" exception that needs a declaration form. Irrelevant for adb installs. | [Play policy](https://support.google.com/googleplay/android-developer/answer/10208820) |
| Xiaomi background setup | Viable (manual) | First-run checklist: Background autostart on, battery saver "No restrictions", app locked in recents. Without these, "background processing simply does not work". | [dontkillmyapp](https://dontkillmyapp.com/xiaomi) |
| Enable accessibility on a sideloaded app | Viable (manual) | Android 13+ can block it as a restricted setting. Unblock in Settings > Apps > MAMÁ > More > Allow restricted settings. | [Android Help](https://support.google.com/android/answer/12623953) |

**extra:1** (#25) depends on the offline SMS row. **extra:2** (#26) depends on the shutdown, airplane mode and heartbeat rows. The trigger spike (#10) has posted no results yet, so its owner must confirm the Xiaomi rows on the demo phone.

## Roadmap slide

- Built-in Google Emergency SOS integration (no documented third-party hook today).
- Blocking shutdown while an alert runs (needs device-owner or MDM enrollment).
- Play Store release with SMS fallback (needs Play's emergency SMS exception).
