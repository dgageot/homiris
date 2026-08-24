package hass

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTopics() Topics {
	return Topics{DiscoveryPrefix: "homeassistant", Prefix: "homiris", NodeID: "homiris", DeviceName: "Homiris"}
}

func reading(key, field string, value any) Reading {
	return Reading{Key: key, Label: strings.ReplaceAll(key, "_", " "), Field: field, Value: value}
}

func TestTopics(t *testing.T) {
	topics := testTopics()

	assert.Equal(t, "homiris/homiris/state", topics.State())
	assert.Equal(t, "homiris/homiris/status", topics.Availability())
	assert.Equal(t, "homeassistant/sensor/homiris/alarm_state/config",
		topics.Discovery(Entity{Component: "sensor", ObjectID: "alarm_state"}))
}

func TestEntitiesAreSortedAndReadOnly(t *testing.T) {
	entities := testTopics().Entities(map[string]Reading{
		"peripherals_salon_temperature": reading("peripherals_salon_temperature", "temperature", json.Number("21.5")),
		"alarm_state":                   reading("alarm_state", "alarm_state", "armed_away"),
		"armed":                         reading("armed", "armed", true),
	})

	require.Len(t, entities, 3)
	assert.Equal(t, []string{"alarm_state", "armed", "peripherals_salon_temperature"},
		[]string{entities[0].ObjectID, entities[1].ObjectID, entities[2].ObjectID})
	for _, entity := range entities {
		assert.NotContains(t, entity.Config, "command_topic")
		assert.Equal(t, "homiris/homiris/state", entity.Config["state_topic"])
		assert.Equal(t, "homiris/homiris/status", entity.Config["availability_topic"])
	}
}

func TestEntityForBoolean(t *testing.T) {
	entity := testTopics().entity(reading("peripherals_salon_opened", "opened", true))

	assert.Equal(t, "binary_sensor", entity.Component)
	assert.Equal(t, "opening", entity.Config["device_class"])
	assert.Equal(t, "{{ value_json['peripherals_salon_opened'] | lower }}", entity.Config["value_template"])
	assert.Equal(t, "true", entity.Config["payload_on"])
	assert.Equal(t, "false", entity.Config["payload_off"])
	assert.Equal(t, "homiris_peripherals_salon_opened", entity.Config["unique_id"])
}

// A peripheral named "Porte d'entrée" must not make its battery flag look like
// an opening sensor: only the API field name is classified.
func TestEntityIgnoresPeripheralNameWhenClassifying(t *testing.T) {
	entity := testTopics().entity(reading("peripherals_porte_d_entree_battery_default", "battery_default", true))

	assert.Equal(t, "battery", entity.Config["device_class"])
}

func TestEntityForFailureFlagIsAProblem(t *testing.T) {
	entity := testTopics().entity(reading("system_last_state_ac_failure", "ac_failure", true))

	assert.Equal(t, "binary_sensor", entity.Component)
	assert.Equal(t, "problem", entity.Config["device_class"])
}

func TestEntityForTemperature(t *testing.T) {
	entity := testTopics().entity(reading("peripherals_salon_temperature", "temperature", json.Number("21.5")))

	assert.Equal(t, "sensor", entity.Component)
	assert.Equal(t, "temperature", entity.Config["device_class"])
	assert.Equal(t, "°C", entity.Config["unit_of_measurement"])
	assert.Equal(t, "measurement", entity.Config["state_class"])
	assert.NotContains(t, entity.Config, "entity_category")
}

func TestEntityForRawNumberIsDiagnosticWithoutStateClass(t *testing.T) {
	entity := testTopics().entity(reading("system_last_state_type_code", "type_code", json.Number("7")))

	assert.Equal(t, "sensor", entity.Component)
	assert.Equal(t, "diagnostic", entity.Config["entity_category"])
	assert.NotContains(t, entity.Config, "state_class")
}

func TestEntityForGSMLevel(t *testing.T) {
	entity := testTopics().entity(reading("system_last_state_gsm_level", "gsm_level", json.Number("4")))

	assert.Equal(t, "measurement", entity.Config["state_class"])
	assert.Equal(t, "mdi:signal", entity.Config["icon"])
}

// A field named after a measurement but holding text must not pretend to be a
// measurement, Home Assistant would reject its state.
func TestEntityForTextualTemperature(t *testing.T) {
	entity := testTopics().entity(reading("peripherals_salon_temperature", "temperature", "N/A"))

	assert.Equal(t, "sensor", entity.Component)
	assert.NotContains(t, entity.Config, "device_class")
	assert.NotContains(t, entity.Config, "unit_of_measurement")
	assert.NotContains(t, entity.Config, "state_class")
}

func TestEntityForTextHasNoStateClass(t *testing.T) {
	entity := testTopics().entity(reading("system_last_state_battery_status", "battery_status", "OK"))

	assert.NotContains(t, entity.Config, "state_class")
	assert.NotContains(t, entity.Config, "unit_of_measurement")
	assert.Equal(t, "System last state battery status", entity.Config["name"])
}

func TestEntityForISODateIsATimestamp(t *testing.T) {
	entity := testTopics().entity(reading("last_events_0_date", "date", "2026-08-24T09:12:00Z"))

	assert.Equal(t, "timestamp", entity.Config["device_class"])

	notADate := testTopics().entity(reading("last_events_0_date", "date", "24/08/2026"))

	assert.NotContains(t, notADate.Config, "device_class")
}

func TestEntityForAlarmStateIsAnEnum(t *testing.T) {
	entity := testTopics().entity(reading(KeyAlarmState, KeyAlarmState, "armed_away"))

	assert.Equal(t, "sensor", entity.Component)
	assert.Equal(t, "enum", entity.Config["device_class"])
	assert.Contains(t, entity.Config["options"], "triggered")
	assert.NotContains(t, entity.Config, "entity_category")
}

func TestEntityNameUsesPeripheralLabel(t *testing.T) {
	entity := testTopics().entity(Reading{
		Key:   "peripherals_42_temperature",
		Label: "peripherals Porte d'entrée temperature",
		Field: "temperature",
		Value: json.Number("19.5"),
	})

	assert.Equal(t, "Peripherals Porte d'entrée temperature", entity.Config["name"])
	assert.Equal(t, "homiris_peripherals_42_temperature", entity.Config["unique_id"])
}

func TestEntityConfigIsValidJSON(t *testing.T) {
	entity := testTopics().entity(reading(KeyLastUpdated, KeyLastUpdated, "2026-08-24T10:00:00Z"))

	payload, err := json.Marshal(entity.Config)
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"device_class":"timestamp"`)
}
