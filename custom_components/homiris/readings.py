"""Turn a Homiris homepage payload into the values exposed to Home Assistant.

This module holds no Home Assistant dependency: the API describes neither its
fields nor their meaning, so everything here is about walking an unknown payload
and guessing as little as possible.
"""

from __future__ import annotations

import unicodedata
from dataclasses import dataclass
from typing import Any

# Longest state Home Assistant accepts.
MAX_STATE_LENGTH = 255

# Fragments matched anywhere in a field name: no legitimate alarm field contains
# them, and they must never reach Home Assistant.
SENSITIVE_FRAGMENTS = (
    "token",
    "password",
    "passwd",
    "pwd",
    "secret",
    "session",
    "iban",
    "mail",
    "firstname",
    "lastname",
    "prenom",
    "civilite",
    "birth",
    "naissance",
    "latitude",
    "longitude",
    "avatar",
    "photo",
    "iccid",
    "imei",
)

# Field names dropped with everything below them, matched exactly: these words
# also start harmless names such as "telecommande" or "contactType".
SENSITIVE_FIELDS = frozenset(
    {
        "login",
        "user",
        "users",
        "utilisateur",
        "person",
        "personne",
        "people",
        "contact",
        "contacts",
        "customer",
        "client",
        "clients",
        "abonne",
        "abonnement",
        "contract",
        "contracts",
        "contrat",
        "contrats",
        "billing",
        "facture",
        "invoice",
        "payment",
        "address",
        "addresses",
        "adresse",
        "adresses",
        "city",
        "ville",
        "postal",
        "code_postal",
        "zip",
        "country",
        "pays",
        "phone",
        "phone_number",
        "tel",
        "telephone",
        "mobile",
        "sim",
        "url",
        "href",
        "image",
        "images",
    }
)

# Fields that identify an array element in a way that survives a rename.
ID_KEYS = ("id", "idDevice", "idPeripheral", "idProduct", "idZone", "reference", "code")

# Fields holding the name a user would recognize.
LABEL_KEYS = ("name", "label", "nom", "libelle")


@dataclass(frozen=True, slots=True)
class Reading:
    """A single value exposed as an entity."""

    key: str
    """Stable path, used as the entity unique id."""

    label: str
    """Human readable path, used as the entity name."""

    field: str
    """Name of the API field, used to guess what the value means."""

    value: Any


def flatten(payload: Any) -> dict[str, Reading]:
    """Reduce a payload to its leaf values, keyed by a slugified path.

    Array elements are keyed by their identifier when they all have a unique
    one, by position otherwise, so that entities stay stable when peripherals
    are renamed or events scroll by.
    """
    readings: dict[str, Reading] = {}
    _flatten(Reading(key="", label="", field="", value=None), payload, readings)
    return readings


def _flatten(path: Reading, value: Any, readings: dict[str, Reading]) -> None:
    if isinstance(value, dict):
        for key in sorted(value):
            if is_sensitive(key):
                continue
            name = slug(key)
            _flatten(_descend(path, name, name.replace("_", " "), name), value[key], readings)
        return

    if isinstance(value, list):
        names = _element_names(value)
        for index, item in enumerate(value):
            _flatten(
                _descend(path, names[index], _element_label(item, index), path.field),
                item,
                readings,
            )
        return

    if isinstance(value, (bool, int, float)):
        _add(readings, path, value)
        return

    if isinstance(value, str) and value and len(value) <= MAX_STATE_LENGTH:
        _add(readings, path, value)


def _descend(path: Reading, key: str, label: str, field: str) -> Reading:
    return Reading(
        key=_join(path.key, key, "_"),
        label=_join(path.label, label, " "),
        field=field,
        value=None,
    )


def _add(readings: dict[str, Reading], path: Reading, value: Any) -> None:
    if not path.key:
        return
    readings[path.key] = Reading(key=path.key, label=path.label, field=path.field, value=value)


def _element_names(items: list[Any]) -> list[str]:
    """Key array elements by identifier, or fall back to positions.

    As soon as one identifier is missing or repeated the whole array falls back
    to positions: a reading must never be overwritten by a sibling.
    """
    names: list[str] = []
    for item in items:
        identifier = _element_id(item)
        if not identifier or identifier in names:
            return [str(index) for index in range(len(items))]
        names.append(identifier)
    return names


def _element_id(item: Any) -> str:
    if not isinstance(item, dict):
        return ""
    for key in ID_KEYS:
        value = item.get(key)
        if isinstance(value, bool):
            continue
        if isinstance(value, (int, float)):
            return slug(str(value))
        if isinstance(value, str) and (identifier := slug(value)):
            return identifier
    return ""


def _element_label(item: Any, index: int) -> str:
    if isinstance(item, dict):
        for key in LABEL_KEYS:
            value = item.get(key)
            if isinstance(value, str) and value:
                return value
    return str(index)


def is_sensitive(key: str) -> bool:
    """Return whether a field holds credentials or personal data."""
    slugged = slug(key)
    if slugged in SENSITIVE_FIELDS:
        return True
    return any(fragment in slugged for fragment in SENSITIVE_FRAGMENTS)


def redact(payload: Any) -> Any:
    """Return a copy of a payload without its sensitive fields."""
    if isinstance(payload, dict):
        return {
            key: "**REDACTED**" if is_sensitive(key) else redact(value)
            for key, value in payload.items()
        }
    if isinstance(payload, list):
        return [redact(item) for item in payload]
    return payload


def slug(value: str) -> str:
    """Turn a camelCase or accented label into a snake_case identifier."""
    folded = unicodedata.normalize("NFKD", value)
    characters = [character for character in folded if not unicodedata.combining(character)]

    slugged: list[str] = []
    for index, character in enumerate(characters):
        if _starts_word(characters, index):
            slugged.append("_")
        lowered = character.lower()
        slugged.append(lowered if lowered.isascii() and lowered.isalnum() else "_")

    return "_".join(part for part in "".join(slugged).split("_") if part)


def _starts_word(characters: list[str], index: int) -> bool:
    """Report whether a word starts here, as in "securityMode" or "GSMLevel"."""
    if index == 0 or not characters[index].isupper():
        return False
    previous = characters[index - 1]
    if previous.islower() or previous.isdigit():
        return True
    return previous.isupper() and index + 1 < len(characters) and characters[index + 1].islower()


def _join(prefix: str, part: str, separator: str) -> str:
    if not prefix:
        return part
    if not part:
        return prefix
    return f"{prefix}{separator}{part}"
