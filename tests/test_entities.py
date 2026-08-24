"""Tests for the entities built from the Homiris payload."""

from __future__ import annotations

import copy
from datetime import timedelta

from freezegun.api import FrozenDateTimeFactory
from homeassistant.config_entries import ConfigEntryState
from homeassistant.const import (
    ATTR_DEVICE_CLASS,
    ATTR_UNIT_OF_MEASUREMENT,
    STATE_OFF,
    STATE_ON,
    STATE_UNAVAILABLE,
    STATE_UNKNOWN,
    EntityCategory,
)
from homeassistant.core import HomeAssistant
from homeassistant.helpers import entity_registry as er
from pytest_homeassistant_custom_component.common import MockConfigEntry, async_fire_time_changed
from pytest_homeassistant_custom_component.components.diagnostics import (
    get_diagnostics_for_config_entry,
)
from pytest_homeassistant_custom_component.test_util.aiohttp import AiohttpClientMocker
from pytest_homeassistant_custom_component.typing import ClientSessionGenerator

from .conftest import HOMEPAGE, TOKEN_URL, mock_api


async def setup_entry(hass: HomeAssistant, entry: MockConfigEntry) -> None:
    """Set up the integration and wait for its entities."""
    entry.add_to_hass(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


async def test_alarm_entities(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """The state of the system is exposed as an enum sensor and two flags."""
    await setup_entry(hass, entry)

    state = hass.states.get("sensor.homiris_123456_alarm_state")
    assert state is not None
    assert state.state == "armed_away"
    assert state.attributes[ATTR_DEVICE_CLASS] == "enum"
    assert "triggered" in state.attributes["options"]

    assert hass.states.get("sensor.homiris_123456_security_mode").state == "TOTAL"
    assert hass.states.get("binary_sensor.homiris_123456_armed").state == STATE_ON
    assert hass.states.get("binary_sensor.homiris_123456_triggered").state == STATE_OFF


async def test_peripheral_entities(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """Peripherals are keyed by identifier and named after their label."""
    await setup_entry(hass, entry)

    temperature = hass.states.get("sensor.homiris_123456_peripherals_porte_d_entree_temperature")
    assert temperature.state == "19.5"
    assert temperature.attributes[ATTR_DEVICE_CLASS] == "temperature"
    assert temperature.attributes[ATTR_UNIT_OF_MEASUREMENT] == "°C"
    assert temperature.attributes["friendly_name"] == (
        "Homiris 123456 Peripherals Porte d'entrée temperature"
    )

    opened = hass.states.get("binary_sensor.homiris_123456_peripherals_porte_d_entree_opened")
    assert opened.state == STATE_OFF
    assert opened.attributes[ATTR_DEVICE_CLASS] == "opening"


async def test_text_that_looks_like_a_measurement_stays_text(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """A field named "temperatureExt" holding "N/A" is not a measurement."""
    await setup_entry(hass, entry)

    state = hass.states.get("sensor.homiris_123456_peripherals_porte_d_entree_temperature_ext")
    assert state.state == "N/A"
    assert ATTR_DEVICE_CLASS not in state.attributes
    assert ATTR_UNIT_OF_MEASUREMENT not in state.attributes


async def test_event_dates_are_timestamps(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """ISO 8601 strings are exposed as timestamps."""
    await setup_entry(hass, entry)

    state = hass.states.get("sensor.homiris_123456_last_events_mise_en_marche_totale_date")
    assert state.attributes[ATTR_DEVICE_CLASS] == "timestamp"
    assert state.state == "2026-08-24T09:12:00+00:00"


async def test_raw_fields_are_diagnostics(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """Raw API fields do not clutter the dashboard, unlike temperatures."""
    await setup_entry(hass, entry)
    registry = er.async_get(hass)

    gsm = registry.async_get("sensor.homiris_123456_system_last_state_gsm_level")
    assert gsm.entity_category is EntityCategory.DIAGNOSTIC

    temperature = registry.async_get("sensor.homiris_123456_peripherals_porte_d_entree_temperature")
    assert temperature.entity_category is None


async def test_credentials_are_never_exposed(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """Session identifiers and personal data are not turned into entities."""
    await setup_entry(hass, entry)

    assert hass.states.get("sensor.homiris_123456_id_session") is None
    assert hass.states.get("sensor.homiris_123456_contact_email") is None


async def test_unknown_mode_is_unknown(
    hass: HomeAssistant, aioclient_mock: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """An unexpected mode does not break the enum sensor."""
    homepage = copy.deepcopy(HOMEPAGE)
    homepage["systemLastState"]["securityMode"] = "SOMETHING_NEW"
    mock_api(aioclient_mock, homepage)

    await setup_entry(hass, entry)

    assert hass.states.get("sensor.homiris_123456_alarm_state").state == STATE_UNKNOWN
    assert hass.states.get("sensor.homiris_123456_security_mode").state == "SOMETHING_NEW"
    assert hass.states.get("binary_sensor.homiris_123456_armed").state == STATE_OFF


async def test_entities_follow_the_payload(
    hass: HomeAssistant,
    aioclient_mock: AiohttpClientMocker,
    entry: MockConfigEntry,
    freezer: FrozenDateTimeFactory,
) -> None:
    """A peripheral appearing later gets an entity; one that goes away goes unavailable."""
    homepage = copy.deepcopy(HOMEPAGE)
    del homepage["peripherals"]
    mock_api(aioclient_mock, homepage)
    await setup_entry(hass, entry)
    assert hass.states.get("sensor.homiris_123456_peripherals_porte_d_entree_temperature") is None

    mock_api(aioclient_mock)
    freezer.tick(timedelta(minutes=5))
    async_fire_time_changed(hass)
    await hass.async_block_till_done()

    temperature = "sensor.homiris_123456_peripherals_porte_d_entree_temperature"
    assert hass.states.get(temperature).state == "19.5"

    mock_api(aioclient_mock, homepage)
    freezer.tick(timedelta(minutes=5))
    async_fire_time_changed(hass)
    await hass.async_block_till_done()

    assert hass.states.get(temperature).state == STATE_UNAVAILABLE


async def test_expired_credentials_ask_for_reauthentication(
    hass: HomeAssistant, aioclient_mock: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """A rejected login starts a reauth flow instead of silently retrying."""
    aioclient_mock.post(TOKEN_URL, status=401, text="invalid client")

    entry.add_to_hass(hass)
    await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    assert entry.state is ConfigEntryState.SETUP_ERROR
    assert any(
        flow["context"]["source"] == "reauth" for flow in hass.config_entries.flow.async_progress()
    )


async def test_unload(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """Unloading removes the entities."""
    await setup_entry(hass, entry)

    assert await hass.config_entries.async_unload(entry.entry_id)
    await hass.async_block_till_done()

    assert entry.state is ConfigEntryState.NOT_LOADED
    assert hass.states.get("sensor.homiris_123456_alarm_state").state == STATE_UNAVAILABLE


async def test_diagnostics(
    hass: HomeAssistant,
    api: AiohttpClientMocker,
    entry: MockConfigEntry,
    hass_client: ClientSessionGenerator,
) -> None:
    """Diagnostics show the payload without the personal data."""
    await setup_entry(hass, entry)

    diagnostics = await get_diagnostics_for_config_entry(hass, hass_client, entry)

    assert diagnostics["payload"]["contact"] == "**REDACTED**"
    assert diagnostics["payload"]["systemLastState"]["securityMode"] == "TOTAL"
    assert diagnostics["readings"]["alarm_state"]["value"] == "armed_away"
