"""Binary sensors for the flags reported by the Homiris API."""

from __future__ import annotations

from homeassistant.components.binary_sensor import (
    BinarySensorDeviceClass,
    BinarySensorEntity,
)
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from .const import KEY_ARMED, KEY_TRIGGERED
from .coordinator import HomirisConfigEntry, HomirisCoordinator
from .entity import HomirisEntity, async_add_readings
from .readings import Reading

# Readings shown on the device page; the raw API flags are diagnostics.
PRIMARY_KEYS = (KEY_ARMED, KEY_TRIGGERED)

# Device class guessed from the API field name, first match wins.
DEVICE_CLASSES: tuple[tuple[BinarySensorDeviceClass, tuple[str, ...]], ...] = (
    (BinarySensorDeviceClass.SMOKE, ("smoke", "fumee", "incendie")),
    (BinarySensorDeviceClass.MOTION, ("motion", "mouvement", "presence", "intrusion")),
    (BinarySensorDeviceClass.OPENING, ("open", "ouvert", "porte", "fenetre")),
    (BinarySensorDeviceClass.TAMPER, ("sabotage", "tamper")),
    (BinarySensorDeviceClass.SAFETY, ("alarm", "alarme")),
    (BinarySensorDeviceClass.BATTERY, ("battery", "batterie")),
    (
        BinarySensorDeviceClass.CONNECTIVITY,
        ("connect", "online", "reachable", "joignable"),
    ),
    (
        BinarySensorDeviceClass.PROBLEM,
        (
            "defaut",
            "default",
            "fault",
            "failure",
            "panne",
            "anomalie",
            "problem",
            "erreur",
            "error",
            "alert",
        ),
    ),
)

VISIBLE_CLASSES = (
    BinarySensorDeviceClass.MOTION,
    BinarySensorDeviceClass.OPENING,
    BinarySensorDeviceClass.SMOKE,
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: HomirisConfigEntry,
    async_add_entities: AddConfigEntryEntitiesCallback,
) -> None:
    """Set up the Homiris binary sensors."""
    coordinator = entry.runtime_data

    def build(reading: Reading) -> HomirisBinarySensor | None:
        if not isinstance(reading.value, bool):
            return None
        return HomirisBinarySensor(coordinator, reading)

    async_add_readings(entry, async_add_entities, build)


class HomirisBinarySensor(HomirisEntity, BinarySensorEntity):
    """A flag reported by the alarm system."""

    def __init__(self, coordinator: HomirisCoordinator, reading: Reading) -> None:
        """Initialize the binary sensor."""
        super().__init__(coordinator, reading)

        device_class: BinarySensorDeviceClass | None = None
        if reading.key == KEY_ARMED:
            self._attr_icon = "mdi:shield-lock"
        elif reading.key == KEY_TRIGGERED:
            device_class = BinarySensorDeviceClass.SAFETY
        else:
            device_class = _device_class(reading.field)

        self._attr_device_class = device_class
        if reading.key not in PRIMARY_KEYS and device_class not in VISIBLE_CLASSES:
            self._attr_entity_category = EntityCategory.DIAGNOSTIC

    @property
    def is_on(self) -> bool | None:
        """Return the state of the flag."""
        value = self._value
        return value if isinstance(value, bool) else None


def _device_class(field: str) -> BinarySensorDeviceClass | None:
    for device_class, needles in DEVICE_CLASSES:
        if any(needle in field for needle in needles):
            return device_class
    return None
