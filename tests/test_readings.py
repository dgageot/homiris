"""Tests for the payload walking, which is where the guessing happens."""

from __future__ import annotations

from custom_components.homiris import alarm
from custom_components.homiris.readings import flatten, is_sensitive, redact, slug


def test_flatten_keys_and_labels() -> None:
    """Every leaf becomes a reading, keyed by identifier and named by label."""
    readings = flatten(
        {
            "systemLastState": {"securityMode": "TOTAL", "gsmLevel": 4, "acFailure": False},
            "peripherals": [
                {"idDevice": "42", "name": "Porte d'entrée", "temperature": 19.5},
                {"idDevice": "43", "name": "Garage", "temperature": 12},
            ],
            "empty": "",
            "missing": None,
        }
    )

    assert set(readings) == {
        "system_last_state_security_mode",
        "system_last_state_gsm_level",
        "system_last_state_ac_failure",
        "peripherals_42_id_device",
        "peripherals_42_name",
        "peripherals_42_temperature",
        "peripherals_43_id_device",
        "peripherals_43_name",
        "peripherals_43_temperature",
    }

    temperature = readings["peripherals_42_temperature"]
    assert temperature.label == "peripherals Porte d'entrée temperature"
    assert temperature.field == "temperature"
    assert temperature.value == 19.5


def test_flatten_falls_back_to_positions_without_unique_identifiers() -> None:
    """Repeated identifiers must not make one element overwrite its sibling."""
    readings = flatten({"lastEvents": [{"code": 7, "label": "A"}, {"code": 7, "label": "B"}]})

    assert readings["last_events_0_label"].value == "A"
    assert readings["last_events_1_label"].value == "B"


def test_flatten_drops_credentials_and_personal_data() -> None:
    """Sensitive subtrees never reach Home Assistant."""
    readings = flatten(
        {
            "idSession": "session-id",
            "accessToken": "token",
            "contact": {"email": "a@b.c", "phoneNumber": "0600000000"},
            "customer": {"city": "Paris"},
            "securityMode": "TOTAL",
        }
    )

    assert set(readings) == {"security_mode"}


def test_flatten_keeps_fields_that_merely_look_sensitive() -> None:
    """Words such as "tel" or "contact" also start harmless field names."""
    readings = flatten({"telecommande": "OK", "contactType": "MAGNETIC", "simulation": False})

    assert set(readings) == {"telecommande", "contact_type", "simulation"}


def test_flatten_drops_overlong_strings() -> None:
    """Home Assistant refuses states longer than 255 characters."""
    readings = flatten({"note": "a" * 256, "short": "ok"})

    assert set(readings) == {"short"}


def test_slug() -> None:
    """Slugs turn camelCase and accented labels into snake_case identifiers."""
    assert slug("securityMode") == "security_mode"
    assert slug("idDevice") == "id_device"
    assert slug("GSMLevel") == "gsm_level"
    assert slug("Porte d'entrée_") == "porte_d_entree"


def test_is_sensitive() -> None:
    """Only an exact match hides an ambiguous field name."""
    assert is_sensitive("accessToken")
    assert is_sensitive("contact")
    assert not is_sensitive("contactType")
    assert not is_sensitive("telecommande")


def test_redact() -> None:
    """Diagnostics keep the payload shape but hide the personal data."""
    assert redact({"contact": {"email": "a@b.c"}, "securityMode": "TOTAL"}) == {
        "contact": "**REDACTED**",
        "securityMode": "TOTAL",
    }


def test_security_mode_is_found_at_any_depth() -> None:
    """The mode is read from its usual place, or looked up anywhere."""
    assert alarm.security_mode({"systemLastState": {"securityMode": "TOTAL"}}) == "TOTAL"
    assert alarm.security_mode({"data": {"systems": [{"securityMode": "ARRET"}]}}) == "ARRET"
    assert alarm.security_mode({}) == ""


def test_alarm_state() -> None:
    """Panel modes map to Home Assistant states, numbered variants included."""
    assert alarm.alarm_state("ARRET") == alarm.STATE_DISARMED
    assert alarm.alarm_state("HORS SERVICE") == alarm.STATE_DISARMED
    assert alarm.alarm_state("PARTIEL 2") == alarm.STATE_ARMED_NIGHT
    assert alarm.alarm_state("TOTALE") == alarm.STATE_ARMED_AWAY
    assert alarm.alarm_state("PRESENCE") == alarm.STATE_ARMED_HOME
    assert alarm.alarm_state("ALARME") == alarm.STATE_TRIGGERED
    assert alarm.alarm_state("MARCHE EN COURS") == alarm.STATE_ARMING
    assert alarm.alarm_state("whatever") == alarm.STATE_UNKNOWN
    assert alarm.alarm_state("") == alarm.STATE_UNKNOWN


def test_is_armed() -> None:
    """Armed means anything but disarmed or unknown."""
    assert alarm.is_armed(alarm.STATE_ARMED_AWAY)
    assert not alarm.is_armed(alarm.STATE_DISARMED)
    assert not alarm.is_armed(alarm.STATE_UNKNOWN)
