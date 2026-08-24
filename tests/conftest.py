"""Fixtures for the Homiris tests."""

from __future__ import annotations

from typing import Any

import pytest
from homeassistant.const import CONF_PASSWORD, CONF_URL, CONF_USERNAME
from pytest_homeassistant_custom_component.common import MockConfigEntry
from pytest_homeassistant_custom_component.test_util.aiohttp import AiohttpClientMocker

from custom_components.homiris.const import CONF_CLIENT_TOKEN, DOMAIN

BASE_URL = "https://homiris.test/"
TOKEN_URL = f"{BASE_URL}token"
CONNECT_URL = f"{BASE_URL}smartphone/production/1.0.0/connect"
HOMEPAGE_URL = f"{BASE_URL}smartphone/production/1.0.0/homepage/session-id"

CREDENTIALS = {
    CONF_USERNAME: "123456",
    CONF_PASSWORD: "secret",
    CONF_CLIENT_TOKEN: "client-token",
    CONF_URL: BASE_URL,
}

HOMEPAGE: dict[str, Any] = {
    "idSession": "session-id",
    "contact": {"email": "someone@example.com"},
    "systemLastState": {
        "securityMode": "TOTAL",
        "gsmLevel": 4,
        "batteryLevel": 95,
        "acFailure": False,
    },
    "peripherals": [
        {
            "idDevice": "42",
            "name": "Porte d'entrée",
            "opened": False,
            "temperature": 19.5,
            "temperatureExt": "N/A",
        }
    ],
    "lastEvents": [{"label": "Mise en marche totale", "date": "2026-08-24T09:12:00Z"}],
}


@pytest.fixture(autouse=True)
def auto_enable_custom_integrations(enable_custom_integrations: None) -> None:
    """Make Home Assistant load the integration from custom_components."""


@pytest.fixture
def entry() -> MockConfigEntry:
    """Return a configured Homiris entry."""
    return MockConfigEntry(
        domain=DOMAIN, unique_id="123456", title="Homiris 123456", data=CREDENTIALS
    )


@pytest.fixture
def api(aioclient_mock: AiohttpClientMocker) -> AiohttpClientMocker:
    """Answer like the Homiris API does."""
    mock_api(aioclient_mock)
    return aioclient_mock


def mock_api(aioclient_mock: AiohttpClientMocker, homepage: dict[str, Any] | None = None) -> None:
    """Register the three calls the client makes."""
    aioclient_mock.clear_requests()
    aioclient_mock.post(TOKEN_URL, json={"access_token": "access-token"})
    aioclient_mock.post(CONNECT_URL, json={"idSession": "session-id"})
    aioclient_mock.get(HOMEPAGE_URL, json=HOMEPAGE if homepage is None else homepage)
