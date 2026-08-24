"""Sensors exposing everything the Homiris API reports."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from homeassistant.components.sensor import (
    SensorDeviceClass,
    SensorEntity,
    SensorStateClass,
)
from homeassistant.const import PERCENTAGE, EntityCategory, UnitOfTemperature
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback
from homeassistant.util import dt as dt_util

from .alarm import ALARM_STATES
from .const import KEY_ALARM_STATE, KEY_SECURITY_MODE
from .coordinator import HomirisConfigEntry, HomirisCoordinator
from .entity import HomirisEntity, async_add_readings
from .readings import Reading

# Readings shown on the device page; the raw API fields are diagnostics.
PRIMARY_KEYS = (KEY_ALARM_STATE, KEY_SECURITY_MODE)

# Classes Home Assistant renders meaningfully outside of the diagnostic section.
VISIBLE_CLASSES = (SensorDeviceClass.TEMPERATURE, SensorDeviceClass.HUMIDITY)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: HomirisConfigEntry,
    async_add_entities: AddConfigEntryEntitiesCallback,
) -> None:
    """Set up the Homiris sensors."""
    coordinator = entry.runtime_data

    def build(reading: Reading) -> HomirisSensor | None:
        if isinstance(reading.value, bool):
            return None
        return HomirisSensor(coordinator, reading)

    async_add_readings(entry, async_add_entities, build)


class HomirisSensor(HomirisEntity, SensorEntity):
    """A value reported by the alarm system."""

    def __init__(self, coordinator: HomirisCoordinator, reading: Reading) -> None:
        """Initialize the sensor."""
        super().__init__(coordinator, reading)
        self._apply(reading)

    def _apply(self, reading: Reading) -> None:
        """Guess what the reading means from its field name and its type."""
        device_class: SensorDeviceClass | None = None

        if reading.key == KEY_ALARM_STATE:
            device_class = SensorDeviceClass.ENUM
            self._attr_options = list(ALARM_STATES)
            self._attr_icon = "mdi:shield-home"
        elif reading.key == KEY_SECURITY_MODE:
            self._attr_icon = "mdi:shield"
        elif isinstance(reading.value, str):
            if _timestamp(reading.value) is not None:
                device_class = SensorDeviceClass.TIMESTAMP
        elif _matches(reading.field, "temperature"):
            device_class = self._measures(SensorDeviceClass.TEMPERATURE, UnitOfTemperature.CELSIUS)
        elif _matches(reading.field, "humidity", "humidite"):
            device_class = self._measures(SensorDeviceClass.HUMIDITY, PERCENTAGE)
        elif _matches(reading.field, "battery", "batterie"):
            device_class = self._measures(SensorDeviceClass.BATTERY, PERCENTAGE)
        elif _matches(reading.field, "signal", "gsm", "rssi"):
            self._attr_state_class = SensorStateClass.MEASUREMENT
            self._attr_icon = "mdi:signal"

        # Identifiers and codes are numbers too: no state class, so that they
        # stay out of the long term statistics.

        self._attr_device_class = device_class
        if reading.key not in PRIMARY_KEYS and device_class not in VISIBLE_CLASSES:
            self._attr_entity_category = EntityCategory.DIAGNOSTIC

    def _measures(self, device_class: SensorDeviceClass, unit: str) -> SensorDeviceClass:
        self._attr_native_unit_of_measurement = unit
        self._attr_state_class = SensorStateClass.MEASUREMENT
        return device_class

    @property
    def native_value(self) -> Any:
        """Return the current value."""
        value = self._value
        if self.device_class is SensorDeviceClass.TIMESTAMP and isinstance(value, str):
            return _timestamp(value)
        return value


def _timestamp(value: str) -> datetime | None:
    """Return an aware datetime, which is the only form a timestamp can take."""
    parsed = dt_util.parse_datetime(value)
    if parsed is None or parsed.tzinfo is None:
        return None
    return parsed


def _matches(field: str, *needles: str) -> bool:
    return any(needle in field for needle in needles)
