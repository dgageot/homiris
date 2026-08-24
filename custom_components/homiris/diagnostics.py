"""Diagnostics for the Homiris integration."""

from __future__ import annotations

from typing import Any

from homeassistant.core import HomeAssistant

from .coordinator import HomirisConfigEntry
from .readings import redact


async def async_get_config_entry_diagnostics(
    hass: HomeAssistant, entry: HomirisConfigEntry
) -> dict[str, Any]:
    """Return the raw API payload, without the credentials and personal data.

    Useful to see which fields an installation actually reports, and to ask for
    a new entity to be exposed.
    """
    coordinator = entry.runtime_data
    return {
        "payload": redact(coordinator.payload),
        "readings": {
            key: {"field": reading.field, "value": reading.value}
            for key, reading in sorted(coordinator.data.items())
        },
    }
