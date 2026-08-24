"""Tests for the Homiris config flow."""

from __future__ import annotations

from homeassistant import config_entries
from homeassistant.const import CONF_PASSWORD, CONF_URL, CONF_USERNAME
from homeassistant.core import HomeAssistant
from homeassistant.data_entry_flow import FlowResultType
from pytest_homeassistant_custom_component.common import MockConfigEntry
from pytest_homeassistant_custom_component.test_util.aiohttp import AiohttpClientMocker

from custom_components.homiris.const import CONF_PHONE_TYPE, CONF_SCAN_INTERVAL, DOMAIN

from .conftest import BASE_URL, CREDENTIALS, TOKEN_URL, mock_api


async def test_user_flow(hass: HomeAssistant, api: AiohttpClientMocker) -> None:
    """The credentials are checked against the API before the entry is created."""
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER}
    )
    assert result["type"] is FlowResultType.FORM
    assert result["step_id"] == "user"

    result = await hass.config_entries.flow.async_configure(result["flow_id"], CREDENTIALS)
    await hass.async_block_till_done()

    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["title"] == "Homiris 123456"
    assert result["data"] == {
        CONF_USERNAME: "123456",
        CONF_PASSWORD: "secret",
        CONF_PHONE_TYPE: "",
        CONF_URL: BASE_URL,
    }
    assert result["result"].unique_id == "123456"


async def test_user_flow_reports_invalid_credentials(
    hass: HomeAssistant, aioclient_mock: AiohttpClientMocker
) -> None:
    """A rejected login is reported on the form, not crashed on."""
    aioclient_mock.post(TOKEN_URL, status=401, text="invalid client")

    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER}, data=CREDENTIALS
    )

    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": "invalid_auth"}

    # Fixing the credentials creates the entry.
    mock_api(aioclient_mock)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], CREDENTIALS)
    assert result["type"] is FlowResultType.CREATE_ENTRY


async def test_user_flow_reports_unreachable_api(
    hass: HomeAssistant, aioclient_mock: AiohttpClientMocker
) -> None:
    """A broken API is reported on the form."""
    aioclient_mock.post(TOKEN_URL, status=500, text="boom")

    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER}, data=CREDENTIALS
    )

    assert result["errors"] == {"base": "cannot_connect"}


async def test_user_flow_refuses_the_same_account_twice(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """One entry per Homiris account."""
    entry.add_to_hass(hass)

    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_USER}, data=CREDENTIALS
    )

    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"


async def test_reauth_flow(
    hass: HomeAssistant, aioclient_mock: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """Expired credentials can be replaced without deleting the entry."""
    entry.add_to_hass(hass)
    mock_api(aioclient_mock)

    result = await entry.start_reauth_flow(hass)
    assert result["step_id"] == "reauth_confirm"

    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_PASSWORD: "new-secret"}
    )
    await hass.async_block_till_done()

    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "reauth_successful"
    assert entry.data[CONF_PASSWORD] == "new-secret"


async def test_options_flow(
    hass: HomeAssistant, api: AiohttpClientMocker, entry: MockConfigEntry
) -> None:
    """The polling interval can be changed after the fact."""
    entry.add_to_hass(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    result = await hass.config_entries.options.async_init(entry.entry_id)
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {CONF_SCAN_INTERVAL: 120}
    )
    await hass.async_block_till_done()

    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert entry.options == {CONF_SCAN_INTERVAL: 120}
    assert entry.runtime_data.update_interval.total_seconds() == 120
