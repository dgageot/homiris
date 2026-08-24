"""Entities built from whatever the Homiris API reports."""

from __future__ import annotations

from collections.abc import Callable, Iterable
from typing import Any

from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.entity import Entity
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN, MANUFACTURER, MODEL
from .coordinator import HomirisConfigEntry, HomirisCoordinator
from .readings import Reading


class HomirisEntity(CoordinatorEntity[HomirisCoordinator]):
    """Base entity mirroring one reading."""

    _attr_has_entity_name = True

    def __init__(self, coordinator: HomirisCoordinator, reading: Reading) -> None:
        """Initialize the entity."""
        super().__init__(coordinator)
        entry = coordinator.config_entry
        self._key = reading.key
        self._attr_unique_id = f"{entry.entry_id}_{reading.key}"
        self._attr_name = _capitalize(reading.label or reading.key)
        self._attr_device_info = DeviceInfo(
            identifiers={(DOMAIN, entry.entry_id)},
            name=entry.title,
            manufacturer=MANUFACTURER,
            model=MODEL,
        )

    @property
    def available(self) -> bool:
        """Return whether the API still reports this reading."""
        return super().available and self._key in self.coordinator.data

    @property
    def _value(self) -> Any:
        """Return the last value reported for this reading."""
        reading = self.coordinator.data.get(self._key)
        return reading.value if reading else None


def _capitalize(label: str) -> str:
    """Capitalize the first letter, leaving peripheral names alone."""
    return label[:1].upper() + label[1:]


def async_add_readings(
    entry: HomirisConfigEntry,
    async_add_entities: AddConfigEntryEntitiesCallback,
    build: Callable[[Reading], Entity | None],
) -> None:
    """Add an entity per reading, including the ones appearing later.

    Which peripherals and events the API reports changes over time, so the
    platforms keep listening instead of only looking at the first answer.
    """
    coordinator = entry.runtime_data
    known: set[str] = set()

    def _add_new() -> None:
        entities = list(_build_new(coordinator.data.values(), known, build))
        if entities:
            async_add_entities(entities)

    entry.async_on_unload(coordinator.async_add_listener(_add_new))
    _add_new()


def _build_new(
    readings: Iterable[Reading],
    known: set[str],
    build: Callable[[Reading], Entity | None],
) -> Iterable[Entity]:
    for reading in readings:
        if reading.key in known:
            continue
        entity = build(reading)
        if entity is None:
            continue
        known.add(reading.key)
        yield entity
