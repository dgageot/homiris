"""Config flow for the Homiris integration."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

import voluptuous as vol
from homeassistant.config_entries import (
    ConfigFlow,
    ConfigFlowResult,
    OptionsFlow,
)
from homeassistant.const import CONF_PASSWORD, CONF_URL, CONF_USERNAME
from homeassistant.core import callback
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.selector import (
    NumberSelector,
    NumberSelectorConfig,
    NumberSelectorMode,
    TextSelector,
    TextSelectorConfig,
    TextSelectorType,
)

from .api import (
    DEFAULT_BASE_URL,
    HomirisAuthError,
    HomirisClient,
    HomirisConnectionError,
)
from .const import (
    CONF_CLIENT_TOKEN,
    CONF_PHONE_TYPE,
    CONF_SCAN_INTERVAL,
    DEFAULT_SCAN_INTERVAL,
    DOMAIN,
    MAX_SCAN_INTERVAL,
    MIN_SCAN_INTERVAL,
)
from .coordinator import HomirisConfigEntry

_PASSWORD_FIELD = TextSelector(TextSelectorConfig(type=TextSelectorType.PASSWORD))

STEP_USER_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_USERNAME): TextSelector(),
        vol.Required(CONF_PASSWORD): _PASSWORD_FIELD,
        vol.Required(CONF_CLIENT_TOKEN): _PASSWORD_FIELD,
        vol.Optional(CONF_PHONE_TYPE, default=""): TextSelector(),
        vol.Optional(CONF_URL, default=DEFAULT_BASE_URL): TextSelector(),
    }
)

STEP_REAUTH_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_PASSWORD): _PASSWORD_FIELD,
        vol.Required(CONF_CLIENT_TOKEN): _PASSWORD_FIELD,
    }
)

OPTIONS_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_SCAN_INTERVAL, default=DEFAULT_SCAN_INTERVAL): NumberSelector(
            NumberSelectorConfig(
                min=MIN_SCAN_INTERVAL,
                max=MAX_SCAN_INTERVAL,
                step=5,
                unit_of_measurement="s",
                mode=NumberSelectorMode.BOX,
            )
        )
    }
)


class HomirisConfigFlow(ConfigFlow, domain=DOMAIN):
    """Ask for the Homiris credentials and check them."""

    VERSION = 1

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Handle the initial step."""
        errors: dict[str, str] = {}
        if user_input is not None:
            errors = await self._async_check(user_input)
            if not errors:
                await self.async_set_unique_id(user_input[CONF_USERNAME])
                self._abort_if_unique_id_configured()
                return self.async_create_entry(
                    title=f"Homiris {user_input[CONF_USERNAME]}", data=user_input
                )

        return self.async_show_form(
            step_id="user",
            data_schema=self.add_suggested_values_to_schema(STEP_USER_SCHEMA, user_input),
            errors=errors,
        )

    async def async_step_reauth(self, entry_data: Mapping[str, Any]) -> ConfigFlowResult:
        """Handle credentials that stopped working."""
        return await self.async_step_reauth_confirm()

    async def async_step_reauth_confirm(
        self, user_input: dict[str, Any] | None = None
    ) -> ConfigFlowResult:
        """Ask for the credentials again."""
        entry = self._get_reauth_entry()
        errors: dict[str, str] = {}
        if user_input is not None:
            data = {**entry.data, **user_input}
            errors = await self._async_check(data)
            if not errors:
                return self.async_update_reload_and_abort(entry, data=data)

        return self.async_show_form(
            step_id="reauth_confirm",
            data_schema=STEP_REAUTH_SCHEMA,
            description_placeholders={CONF_USERNAME: entry.data[CONF_USERNAME]},
            errors=errors,
        )

    async def _async_check(self, user_input: Mapping[str, Any]) -> dict[str, str]:
        """Log in once, to fail at setup time rather than later."""
        client = HomirisClient(
            async_get_clientsession(self.hass),
            login=user_input[CONF_USERNAME],
            password=user_input[CONF_PASSWORD],
            client_token=user_input[CONF_CLIENT_TOKEN],
            phone_type=user_input.get(CONF_PHONE_TYPE, ""),
            base_url=user_input.get(CONF_URL) or DEFAULT_BASE_URL,
        )
        try:
            await client.async_homepage()
        except HomirisAuthError:
            return {"base": "invalid_auth"}
        except HomirisConnectionError:
            return {"base": "cannot_connect"}
        except Exception:
            self.logger.exception("Unexpected error while checking the credentials")
            return {"base": "unknown"}
        return {}

    @staticmethod
    @callback
    def async_get_options_flow(entry: HomirisConfigEntry) -> HomirisOptionsFlow:
        """Return the options flow."""
        return HomirisOptionsFlow()


class HomirisOptionsFlow(OptionsFlow):
    """Let the polling interval be changed."""

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Manage the options."""
        if user_input is not None:
            return self.async_create_entry(
                data={CONF_SCAN_INTERVAL: int(user_input[CONF_SCAN_INTERVAL])}
            )

        return self.async_show_form(
            step_id="init",
            data_schema=self.add_suggested_values_to_schema(
                OPTIONS_SCHEMA, self.config_entry.options
            ),
        )
