"""Read-only client for the Homiris (EPS) smartphone API."""

from __future__ import annotations

from http import HTTPStatus
from typing import Any

from aiohttp import ClientError, ClientResponse, ClientSession, ClientTimeout

APP_VERSION = "4.35.3"
DEFAULT_BASE_URL = "https://y41hsspp-mobile.eps-api.com/"
API_PREFIX = "smartphone/production/1.0.0/"

# Public application credential embedded in the official app. It identifies the
# app, not the subscriber; the API authenticates subscribers separately with
# their login and password.
APP_CLIENT_TOKEN = "NHhSOG1LZFk5OFBRalpkNU1NUzJRNWZYWl9RYTpEajkwbF9IOGs3WGJvZ1pTbzl3MUxTemxOZ01h"

TIMEOUT = ClientTimeout(total=20)


class HomirisError(Exception):
    """Base error raised by the client."""


class HomirisConnectionError(HomirisError):
    """The API could not be reached."""


class HomirisAuthError(HomirisError):
    """The credentials were rejected."""


class HomirisClient:
    """Fetch the read-only state of a Homiris system.

    The access token and the session are cached and renewed when the API rejects
    them, which it does after a while.
    """

    def __init__(
        self,
        session: ClientSession,
        login: str,
        password: str,
        phone_type: str = "",
        base_url: str = DEFAULT_BASE_URL,
    ) -> None:
        """Initialize the client."""
        self._session = session
        self._login = login
        self._password = password
        self._phone_type = phone_type
        self._base_url = base_url.rstrip("/") + "/"
        self._access_token: str | None = None
        self._session_id: str | None = None

    async def async_homepage(self) -> dict[str, Any]:
        """Return everything the API reports: modes, peripherals, events..."""
        access_token, session_id = await self._async_session()
        try:
            return await self._async_get_homepage(access_token, session_id)
        except HomirisAuthError:
            # The session expired: start a new one and try again.
            self.invalidate()

        access_token, session_id = await self._async_session()
        return await self._async_get_homepage(access_token, session_id)

    def invalidate(self) -> None:
        """Forget the cached credentials."""
        self._access_token = None
        self._session_id = None

    async def _async_session(self) -> tuple[str, str]:
        if self._access_token and self._session_id:
            return self._access_token, self._session_id

        self._access_token = await self._async_access_token()
        self._session_id = await self._async_connect(self._access_token)
        return self._access_token, self._session_id

    async def _async_access_token(self) -> str:
        payload = await self._async_request(
            "POST",
            "token",
            headers={
                "Authorization": f"Basic {APP_CLIENT_TOKEN}",
                "Content-Type": "application/x-www-form-urlencoded",
            },
            data={"grant_type": "client_credentials", "scope": "PRODUCTION"},
        )
        token = payload.get("access_token")
        if not isinstance(token, str) or not token:
            raise HomirisAuthError("the API returned no access token")
        return token

    async def _async_connect(self, access_token: str) -> str:
        payload = await self._async_request(
            "POST",
            f"{API_PREFIX}connect",
            headers=self._api_headers(access_token),
            json={
                "application": "SMARTPHONE",
                "typeDevice": "SMARTPHONE",
                "pwd": self._password,
                "login": self._login,
                "originSession": "HOMIRIS",
                "phoneType": self._phone_type,
                "codeLanguage": "FR",
                "version": APP_VERSION,
                "timestamp": "0",
                "system": "",
            },
        )
        session_id = payload.get("idSession")
        if not isinstance(session_id, str) or not session_id:
            raise HomirisAuthError("the API returned no session")
        return session_id

    async def _async_get_homepage(self, access_token: str, session_id: str) -> dict[str, Any]:
        return await self._async_request(
            "GET",
            f"{API_PREFIX}homepage/{session_id}",
            headers=self._api_headers(access_token),
        )

    def _api_headers(self, access_token: str) -> dict[str, str]:
        return {
            "Authorization": f"Bearer {access_token}",
            "Content-Type": "application/json",
            "Eps-Ctx-Source": "MOB-ABO",
            "Eps-Ctx-Username": self._login,
        }

    async def _async_request(
        self,
        method: str,
        path: str,
        *,
        headers: dict[str, str],
        **kwargs: Any,
    ) -> dict[str, Any]:
        request_headers = {
            "Accept": "*/*",
            "User-Agent": f"Homiris/{APP_VERSION}",
            **headers,
        }
        try:
            response = await self._session.request(
                method,
                self._base_url + path,
                headers=request_headers,
                allow_redirects=False,
                timeout=TIMEOUT,
                **kwargs,
            )
            async with response:
                await self._async_raise_for_status(response)
                payload = await response.json(content_type=None)
        except ClientError as error:
            raise HomirisConnectionError(f"cannot reach the Homiris API: {error}") from error
        except TimeoutError as error:
            raise HomirisConnectionError("the Homiris API timed out") from error

        if not isinstance(payload, dict):
            raise HomirisError(f"unexpected answer from {path}")
        return payload

    @staticmethod
    async def _async_raise_for_status(response: ClientResponse) -> None:
        if response.status < 300:
            return
        body = (await response.text())[:200].strip()
        message = f"HTTP {response.status} from {response.url.path}: {body}"
        if response.status in (HTTPStatus.UNAUTHORIZED, HTTPStatus.FORBIDDEN):
            raise HomirisAuthError(message)
        raise HomirisConnectionError(message)
