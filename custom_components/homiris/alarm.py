"""Alarm modes reported by the panel, mapped to Home Assistant states."""

from __future__ import annotations

from typing import Any

STATE_DISARMED = "disarmed"
STATE_ARMED_HOME = "armed_home"
STATE_ARMED_NIGHT = "armed_night"
STATE_ARMED_AWAY = "armed_away"
STATE_ARMING = "arming"
STATE_TRIGGERED = "triggered"
STATE_UNKNOWN = "unknown"

ALARM_STATES = (
    STATE_DISARMED,
    STATE_ARMED_HOME,
    STATE_ARMED_NIGHT,
    STATE_ARMED_AWAY,
    STATE_ARMING,
    STATE_TRIGGERED,
    STATE_UNKNOWN,
)

# Ordered: the first matching prefix wins.
_MAPPINGS: tuple[tuple[str, tuple[str, ...]], ...] = (
    (STATE_TRIGGERED, ("ALARME", "ALARM", "TRIGGERED", "INTRUSION")),
    (STATE_ARMING, ("ARMING", "PENDING")),
    (STATE_DISARMED, ("ARRET", "OFF", "DISARM", "STOP", "NONE", "HORS_SERVICE")),
    (STATE_ARMED_NIGHT, ("PARTIEL", "PARTIAL", "NIGHT", "NUIT")),
    (STATE_ARMED_HOME, ("PRESENCE", "HOME", "ZONE", "PERIMETR")),
    (STATE_ARMED_AWAY, ("TOTAL", "ALL", "AWAY", "ARMED", "MARCHE", "ON")),
)


def security_mode(payload: Any) -> str:
    """Return the mode reported by the panel, e.g. TOTAL or ARRET."""
    if isinstance(payload, dict):
        state = payload.get("systemLastState")
        if isinstance(state, dict):
            mode = state.get("securityMode")
            if isinstance(mode, str) and mode:
                return mode
    return _find(payload, "securityMode")


def alarm_state(mode: str) -> str:
    """Map a panel mode to a Home Assistant alarm state.

    Separators are normalized and modes are matched by prefix, since the panel
    numbers and decorates them, as in "PARTIEL 2" or "MARCHE EN COURS".
    """
    normalized = mode.strip().upper()
    for separator in (" ", "-", "."):
        normalized = normalized.replace(separator, "_")
    if not normalized:
        return STATE_UNKNOWN
    if "EN_COURS" in normalized or "TEMPO" in normalized:
        return STATE_ARMING
    for state, prefixes in _MAPPINGS:
        if normalized.startswith(prefixes):
            return state
    return STATE_UNKNOWN


def is_armed(state: str) -> bool:
    """Return whether the system is armed, in any of its modes."""
    return state not in (STATE_DISARMED, STATE_UNKNOWN)


def _find(payload: Any, key: str) -> str:
    """Return the first non-empty string found under key, whatever its depth."""
    if isinstance(payload, dict):
        value = payload.get(key)
        if isinstance(value, str) and value:
            return value
        for name in sorted(payload):
            if found := _find(payload[name], key):
                return found
    elif isinstance(payload, list):
        for item in payload:
            if found := _find(item, key):
                return found
    return ""
