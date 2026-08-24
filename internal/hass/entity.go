package hass

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"
)

// Entity is a Home Assistant entity described by an MQTT discovery payload.
type Entity struct {
	Component string // sensor or binary_sensor
	ObjectID  string
	Config    map[string]any
}

// Curated readings computed by the bridge on top of the raw payload.
const (
	KeyAlarmState   = "alarm_state"
	KeySecurityMode = "security_mode"
	KeyArmed        = "armed"
	KeyTriggered    = "triggered"
	KeyLastUpdated  = "last_updated"
)

// Topics describes where the bridge publishes.
type Topics struct {
	DiscoveryPrefix string // Home Assistant discovery prefix, usually "homeassistant"
	Prefix          string // bridge topic prefix, usually "homiris"
	NodeID          string // groups the entities of a single Homiris system
	DeviceName      string
}

// State is the topic carrying the JSON document all entities read from.
func (t Topics) State() string { return t.Prefix + "/" + t.NodeID + "/state" }

// Availability is the topic carrying "online" or "offline".
func (t Topics) Availability() string { return t.Prefix + "/" + t.NodeID + "/status" }

// Discovery is the configuration topic of an entity.
func (t Topics) Discovery(entity Entity) string {
	return strings.Join([]string{t.DiscoveryPrefix, entity.Component, t.NodeID, entity.ObjectID, "config"}, "/")
}

func (t Topics) device() map[string]any {
	return map[string]any{
		"identifiers":  []string{t.NodeID},
		"name":         t.DeviceName,
		"manufacturer": "EPS",
		"model":        "Homiris",
	}
}

// Entities describes one read-only entity per reading, sorted by object id.
func (t Topics) Entities(readings map[string]Reading) []Entity {
	entities := make([]Entity, 0, len(readings))
	for _, key := range slices.Sorted(maps.Keys(readings)) {
		entities = append(entities, t.entity(readings[key]))
	}
	return entities
}

func (t Topics) entity(reading Reading) Entity {
	entity := Entity{Component: component(reading.Value), ObjectID: reading.Key}
	entity.Config = map[string]any{
		"name":                  name(reading),
		"unique_id":             t.NodeID + "_" + reading.Key,
		"object_id":             t.NodeID + "_" + reading.Key,
		"state_topic":           t.State(),
		"value_template":        valueTemplate(reading),
		"availability_topic":    t.Availability(),
		"payload_available":     "online",
		"payload_not_available": "offline",
		"device":                t.device(),
		"origin":                map[string]any{"name": "homiris"},
	}
	if entity.Component == "binary_sensor" {
		entity.Config["payload_on"] = "true"
		entity.Config["payload_off"] = "false"
	}
	maps.Copy(entity.Config, classify(reading))
	if isDiagnostic(reading.Key, entity.Config) {
		entity.Config["entity_category"] = "diagnostic"
	}
	return entity
}

func component(value any) string {
	if _, ok := value.(bool); ok {
		return "binary_sensor"
	}
	return "sensor"
}

// valueTemplate reads the entity value out of the shared JSON state. Keys are
// looked up as items, not attributes, so that they cannot collide with the
// methods of a Jinja mapping. Booleans are lowered to match payload_on and
// payload_off.
func valueTemplate(reading Reading) string {
	lookup := "value_json['" + reading.Key + "']"
	if _, ok := reading.Value.(bool); ok {
		return "{{ " + lookup + " | lower }}"
	}
	return "{{ " + lookup + " }}"
}

// classify guesses device class, unit and state class. The value type decides
// first: a field named "temperature" holding "N/A" is text, not a measurement.
// Only the API field name is looked at, never the rest of the path, which holds
// peripheral names.
func classify(reading Reading) map[string]any {
	if curated, ok := curatedClasses[reading.Key]; ok {
		return curated
	}

	switch value := reading.Value.(type) {
	case bool:
		return binaryClass(reading.Field)
	case string:
		return textClass(value)
	default:
		return numberClass(reading.Field)
	}
}

func numberClass(field string) map[string]any {
	switch {
	case contains(field, "temperature"):
		return measurement("temperature", "°C")
	case contains(field, "humidity", "humidite"):
		return measurement("humidity", "%")
	case contains(field, "battery", "batterie"):
		return measurement("battery", "%")
	case contains(field, "signal", "gsm", "rssi"):
		return map[string]any{"state_class": "measurement", "icon": "mdi:signal"}
	default:
		// Codes and identifiers are numbers too: no state class, so that they
		// stay out of the long term statistics.
		return map[string]any{}
	}
}

func measurement(class, unit string) map[string]any {
	return map[string]any{"device_class": class, "unit_of_measurement": unit, "state_class": "measurement"}
}

func textClass(value string) map[string]any {
	if isTimestamp(value) {
		return map[string]any{"device_class": "timestamp"}
	}
	return map[string]any{}
}

func binaryClass(field string) map[string]any {
	switch {
	case contains(field, "smoke", "fumee", "incendie"):
		return map[string]any{"device_class": "smoke"}
	case contains(field, "motion", "mouvement", "presence", "intrusion"):
		return map[string]any{"device_class": "motion"}
	case contains(field, "open", "ouvert", "porte", "fenetre"):
		return map[string]any{"device_class": "opening"}
	case contains(field, "sabotage", "tamper"):
		return map[string]any{"device_class": "tamper"}
	case contains(field, "alarm", "alarme"):
		return map[string]any{"device_class": "safety"}
	case contains(field, "battery", "batterie"):
		return map[string]any{"device_class": "battery"}
	case contains(field, "connect", "online", "reachable", "joignable"):
		return map[string]any{"device_class": "connectivity"}
	case contains(field, "defaut", "default", "fault", "failure", "panne", "anomalie", "problem", "erreur", "error", "alert"):
		return map[string]any{"device_class": "problem"}
	default:
		return map[string]any{}
	}
}

// curatedClasses overrides the guesses for the readings the bridge computes.
var curatedClasses = map[string]map[string]any{
	KeyAlarmState: {
		"name":         "Alarm state",
		"icon":         "mdi:shield-home",
		"device_class": "enum",
		"options":      []string{"disarmed", "armed_home", "armed_night", "armed_away", "arming", "triggered"},
	},
	KeySecurityMode: {"name": "Security mode", "icon": "mdi:shield"},
	KeyArmed:        {"name": "Armed", "icon": "mdi:shield-lock"},
	KeyTriggered:    {"name": "Triggered", "device_class": "safety"},
	KeyLastUpdated:  {"name": "Last updated", "device_class": "timestamp"},
}

// primaryKeys are the computed readings that belong to the main device page
// rather than to the diagnostic section.
var primaryKeys = []string{KeyAlarmState, KeySecurityMode, KeyArmed, KeyTriggered}

// isDiagnostic keeps dashboards readable: raw API fields land in the diagnostic
// section unless Home Assistant can render them meaningfully.
func isDiagnostic(key string, config map[string]any) bool {
	if slices.Contains(primaryKeys, key) {
		return false
	}
	class, _ := config["device_class"].(string)
	return !slices.Contains([]string{"temperature", "humidity", "motion", "smoke", "opening", "moisture", "gas"}, class)
}

// isTimestamp reports whether a value can be published as a timestamp entity,
// which Home Assistant only accepts in ISO 8601 form.
func isTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func contains(field string, needles ...string) bool {
	return slices.ContainsFunc(needles, func(needle string) bool {
		return strings.Contains(field, needle)
	})
}

func name(reading Reading) string {
	words := reading.Label
	if words == "" {
		words = strings.ReplaceAll(reading.Key, "_", " ")
	}
	if words == "" {
		return "Unknown"
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// fingerprint identifies a discovery payload, so that a reading changing type
// republishes its configuration instead of keeping a stale one.
func fingerprint(entity Entity) (string, error) {
	payload, err := json.Marshal(entity.Config)
	if err != nil {
		return "", err
	}
	return entity.Component + "|" + string(payload), nil
}
