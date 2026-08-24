"""The Homiris integration: read-only access to a Homiris (EPS) alarm system."""

from __future__ import annotations

from homeassistant.const import CONF_PASSWORD, CONF_URL, CONF_USERNAME
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import DEFAULT_BASE_URL, HomirisClient
from .const import (
    CONF_CLIENT_TOKEN,
    CONF_PHONE_TYPE,
    CONF_SCAN_INTERVAL,
    DEFAULT_SCAN_INTERVAL,
    PLATFORMS,
)
from .coordinator import HomirisConfigEntry, HomirisCoordinator


async def async_setup_entry(hass: HomeAssistant, entry: HomirisConfigEntry) -> bool:
    """Set up Homiris from a config entry."""
    client = HomirisClient(
        async_get_clientsession(hass),
        login=entry.data[CONF_USERNAME],
        password=entry.data[CONF_PASSWORD],
        client_token=entry.data[CONF_CLIENT_TOKEN],
        phone_type=entry.data.get(CONF_PHONE_TYPE, ""),
        base_url=entry.data.get(CONF_URL, DEFAULT_BASE_URL),
    )
    coordinator = HomirisCoordinator(
        hass,
        entry,
        client,
        entry.options.get(CONF_SCAN_INTERVAL, DEFAULT_SCAN_INTERVAL),
    )
    await coordinator.async_config_entry_first_refresh()

    entry.runtime_data = coordinator
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    entry.async_on_unload(entry.add_update_listener(async_reload_entry))
    return True


async def async_unload_entry(hass: HomeAssistant, entry: HomirisConfigEntry) -> bool:
    """Unload a config entry."""
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)


async def async_reload_entry(hass: HomeAssistant, entry: HomirisConfigEntry) -> None:
    """Reload the entry when its options change."""
    await hass.config_entries.async_reload(entry.entry_id)
