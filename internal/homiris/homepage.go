package homiris

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// Alarm states, borrowed from the Home Assistant vocabulary.
const (
	StateDisarmed   = "disarmed"
	StateArmedHome  = "armed_home"
	StateArmedNight = "armed_night"
	StateArmedAway  = "armed_away"
	StateTriggered  = "triggered"
	StateArming     = "arming"
	StateUnknown    = "unknown"
)

// Homepage is the raw payload of the homepage endpoint.
type Homepage map[string]any

// SecurityMode returns the mode reported by the panel, e.g. TOTAL or ARRET.
func (h Homepage) SecurityMode() string {
	if state, ok := h["systemLastState"].(map[string]any); ok {
		if mode, ok := state["securityMode"].(string); ok && mode != "" {
			return mode
		}
	}
	return findString(map[string]any(h), "securityMode")
}

// AlarmState maps the panel mode to a Home Assistant alarm state, returning
// StateUnknown for modes we do not know about. Separators are normalized and
// modes are matched by prefix, since the panel numbers and decorates them, as
// in "PARTIEL 2" or "MARCHE EN COURS".
func (h Homepage) AlarmState() string {
	mode := normalizeMode(h.SecurityMode())
	if mode == "" {
		return StateUnknown
	}
	if strings.Contains(mode, "EN_COURS") || strings.Contains(mode, "TEMPO") {
		return StateArming
	}
	for _, mapping := range stateMappings {
		if slices.ContainsFunc(mapping.modes, func(prefix string) bool {
			return strings.HasPrefix(mode, prefix)
		}) {
			return mapping.state
		}
	}
	return StateUnknown
}

func normalizeMode(mode string) string {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	for _, separator := range []string{" ", "-", "."} {
		mode = strings.ReplaceAll(mode, separator, "_")
	}
	return mode
}

// stateMappings is ordered: the first matching prefix wins.
var stateMappings = []struct {
	state string
	modes []string
}{
	{StateTriggered, []string{"ALARME", "ALARM", "TRIGGERED", "INTRUSION"}},
	{StateArming, []string{"ARMING", "PENDING"}},
	{StateDisarmed, []string{"ARRET", "OFF", "DISARM", "STOP", "NONE", "HORS_SERVICE"}},
	{StateArmedNight, []string{"PARTIEL", "PARTIAL", "NIGHT", "NUIT"}},
	{StateArmedHome, []string{"PRESENCE", "HOME", "ZONE", "PERIMETR"}},
	{StateArmedAway, []string{"TOTAL", "ALL", "AWAY", "ARMED", "MARCHE", "ON"}},
}

// findString walks the payload and returns the first non-empty string found
// under key, whatever its depth.
func findString(value any, key string) string {
	switch value := value.(type) {
	case map[string]any:
		if found, ok := value[key].(string); ok && found != "" {
			return found
		}
		for _, name := range sortedKeys(value) {
			if found := findString(value[name], key); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range value {
			if found := findString(item, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func sortedKeys(value map[string]any) []string {
	return slices.Sorted(maps.Keys(value))
}

// Indent renders the payload as pretty printed JSON.
func (h Homepage) Indent() ([]byte, error) {
	return json.MarshalIndent(h, "", "  ")
}
