# Homiris for Home Assistant

Read-only Home Assistant integration for a [Homiris](https://www.homiris.fr) (EPS) alarm system.
It polls the mobile app API and exposes everything it reports: alarm state, peripherals,
temperatures, battery and GSM levels, faults, last events.

Arming and disarming are not supported: the API refuses them without the biometric pairing of the
official app.

## Installation

1. In HACS, open the three-dot menu → **Custom repositories**, add `https://github.com/dgageot/homiris`
   with the category **Integration**, then install **Homiris** and restart Home Assistant.
2. **Settings → Devices & services → Add integration → Homiris**, and fill in:
   - **Login**: the account number used in the Homiris app
   - **Password**: the app password
   - **Client token**: the `Basic` authorization token the app sends to the API

The polling interval (60 s by default) can be changed later with **Configure**.

## Entities

One device, **Homiris**, with:

| Entity | Description |
| --- | --- |
| `sensor.homiris_alarm_state` | `disarmed`, `armed_home`, `armed_night`, `armed_away`, `arming` or `triggered` |
| `sensor.homiris_security_mode` | the raw mode reported by the panel, e.g. `TOTAL` |
| `binary_sensor.homiris_armed` | on when the system is armed, in any mode |
| `binary_sensor.homiris_triggered` | on when the alarm is going off |

Plus one entity per field the API returns for your installation: temperatures, battery and signal
levels, opening/smoke/fault flags, last events... Raw fields land in the device's *Diagnostic*
section; credentials and personal data are never exposed.

If a value you care about is missing, download the diagnostics of the integration
(**Devices & services → Homiris → ⋮ → Download diagnostics**): it contains the redacted API payload
and the list of exposed readings.
