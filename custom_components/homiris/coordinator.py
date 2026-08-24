"""Coordinator polling the Homiris API."""

from __future__ import annotations

import logging
from datetime import timedelta
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import ConfigEntryAuthFailed
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from . import alarm
from .api import HomirisAuthError, HomirisClient, HomirisError
from .const import (
    DOMAIN,
    KEY_ALARM_STATE,
    KEY_ARMED,
    KEY_SECURITY_MODE,
    KEY_TRIGGERED,
)
from .readings import Reading, flatten

_LOGGER = logging.getLogger(__name__)

type HomirisConfigEntry = ConfigEntry[HomirisCoordinator]


class HomirisCoordinator(DataUpdateCoordinator[dict[str, Reading]]):
    """Fetch the system state and turn it into readings."""

    def __init__(
        self,
        hass: HomeAssistant,
        entry: HomirisConfigEntry,
        client: HomirisClient,
        scan_interval: int,
    ) -> None:
        """Initialize the coordinator."""
        super().__init__(
            hass,
            _LOGGER,
            config_entry=entry,
            name=DOMAIN,
            update_interval=timedelta(seconds=scan_interval),
        )
        self.client = client
        self.payload: dict[str, Any] = {}

    async def _async_update_data(self) -> dict[str, Reading]:
        try:
            self.payload = await self.client.async_homepage()
        except HomirisAuthError as error:
            raise ConfigEntryAuthFailed(str(error)) from error
        except HomirisError as error:
            raise UpdateFailed(str(error)) from error

        return readings(self.payload)


def readings(payload: dict[str, Any]) -> dict[str, Reading]:
    """Return every leaf of the payload, plus the computed alarm readings."""
    values = flatten(payload)

    mode = alarm.security_mode(payload)
    state = alarm.alarm_state(mode)
    computed = {
        KEY_ALARM_STATE: state,
        KEY_SECURITY_MODE: mode or alarm.STATE_UNKNOWN,
        KEY_ARMED: alarm.is_armed(state),
        KEY_TRIGGERED: state == alarm.STATE_TRIGGERED,
    }
    for key, value in computed.items():
        values[key] = Reading(key=key, label=key.replace("_", " "), field=key, value=value)
    return values
