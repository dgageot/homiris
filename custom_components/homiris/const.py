"""Constants for the Homiris integration."""

from __future__ import annotations

from homeassistant.const import Platform

DOMAIN = "homiris"

PLATFORMS: tuple[Platform, ...] = (Platform.BINARY_SENSOR, Platform.SENSOR)

CONF_PHONE_TYPE = "phone_type"
CONF_SCAN_INTERVAL = "scan_interval"

DEFAULT_SCAN_INTERVAL = 60
MIN_SCAN_INTERVAL = 15
MAX_SCAN_INTERVAL = 3600

MANUFACTURER = "EPS"
MODEL = "Homiris"

# Readings computed by the integration on top of the API payload.
KEY_ALARM_STATE = "alarm_state"
KEY_SECURITY_MODE = "security_mode"
KEY_ARMED = "armed"
KEY_TRIGGERED = "triggered"
