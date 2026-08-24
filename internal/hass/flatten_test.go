package hass

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeNumbers(t *testing.T, payload string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	var decoded map[string]any
	require.NoError(t, decoder.Decode(&decoded))
	return decoded
}

func TestFlatten(t *testing.T) {
	payload := decodeNumbers(t, `{
		"systemLastState": {"securityMode": "TOTAL", "gsmLevel": 4, "acFailure": false},
		"peripherals": [
			{"id": "P1", "name": "Salon", "temperature": 21.5, "batteryLevel": 92},
			{"temperature": 18}
		],
		"empty": "",
		"missing": null
	}`)

	readings := Flatten(payload)

	// The second peripheral has no identifier, so the whole array falls back to
	// positions instead of mixing both schemes.
	assert.Equal(t, map[string]Reading{
		"system_last_state_security_mode": {Key: "system_last_state_security_mode", Label: "system last state security mode", Field: "security_mode", Value: "TOTAL"},
		"system_last_state_gsm_level":     {Key: "system_last_state_gsm_level", Label: "system last state gsm level", Field: "gsm_level", Value: json.Number("4")},
		"system_last_state_ac_failure":    {Key: "system_last_state_ac_failure", Label: "system last state ac failure", Field: "ac_failure", Value: false},
		"peripherals_0_id":                {Key: "peripherals_0_id", Label: "peripherals Salon id", Field: "id", Value: "P1"},
		"peripherals_0_name":              {Key: "peripherals_0_name", Label: "peripherals Salon name", Field: "name", Value: "Salon"},
		"peripherals_0_temperature":       {Key: "peripherals_0_temperature", Label: "peripherals Salon temperature", Field: "temperature", Value: json.Number("21.5")},
		"peripherals_0_battery_level":     {Key: "peripherals_0_battery_level", Label: "peripherals Salon battery level", Field: "battery_level", Value: json.Number("92")},
		"peripherals_1_temperature":       {Key: "peripherals_1_temperature", Label: "peripherals 1 temperature", Field: "temperature", Value: json.Number("18")},
	}, readings)
}

// Keys must survive a peripheral being renamed in the app, otherwise Home
// Assistant ends up with orphan entities.
func TestFlattenKeysUseIdentifiersAndLabelsUseNames(t *testing.T) {
	payload := decodeNumbers(t, `{"peripherals":[
		{"idDevice":"42","name":"Cuisine","opened":true},
		{"idDevice":"43","name":"Garage","opened":false}
	]}`)

	readings := Flatten(payload)

	reading := readings["peripherals_42_opened"]
	assert.Equal(t, "peripherals Cuisine opened", reading.Label)
	assert.Equal(t, "opened", reading.Field)
	assert.Equal(t, true, reading.Value)
	assert.Contains(t, readings, "peripherals_43_opened")
}

// Duplicate identifiers would make one element overwrite its sibling.
func TestFlattenFallsBackToPositionsWhenIdentifiersRepeat(t *testing.T) {
	payload := decodeNumbers(t, `{"lastEvents":[{"code":7,"label":"A"},{"code":7,"label":"B"}]}`)

	readings := Flatten(payload)

	assert.Equal(t, "A", readings["last_events_0_label"].Value)
	assert.Equal(t, "B", readings["last_events_1_label"].Value)
}

func TestFlattenIndexesUnidentifiedElements(t *testing.T) {
	payload := decodeNumbers(t, `{"lastEvents":[{"label":"Mise en marche"}]}`)

	readings := Flatten(payload)

	assert.Contains(t, readings, "last_events_0_label")
	assert.Equal(t, "last events Mise en marche label", readings["last_events_0_label"].Label)
}

func TestFlattenDropsSensitiveKeys(t *testing.T) {
	readings := Flatten(map[string]any{
		"idSession":    "session-id",
		"accessToken":  "token",
		"contact":      map[string]any{"email": "a@b.c", "phoneNumber": "0600000000"},
		"customer":     map[string]any{"city": "Paris"},
		"securityMode": "TOTAL",
	})

	assert.Equal(t, map[string]Reading{
		"security_mode": {Key: "security_mode", Label: "security mode", Field: "security_mode", Value: "TOTAL"},
	}, readings)
}

// Words such as "tel" or "contact" also start harmless field names: only an
// exact match hides a field.
func TestFlattenKeepsFieldsThatMerelyLookSensitive(t *testing.T) {
	readings := Flatten(map[string]any{
		"telecommande": "OK",
		"contactType":  "MAGNETIC",
		"simulation":   false,
	})

	assert.Len(t, readings, 3)
}

func TestFlattenDropsOverlongStrings(t *testing.T) {
	long := make([]byte, maxStateLength+1)
	for i := range long {
		long[i] = 'a'
	}

	readings := Flatten(map[string]any{"note": string(long), "short": "ok"})

	assert.Equal(t, map[string]Reading{
		"short": {Key: "short", Label: "short", Field: "short", Value: "ok"},
	}, readings)
}

func TestSlug(t *testing.T) {
	assert.Equal(t, "security_mode", slug("securityMode"))
	assert.Equal(t, "id_device", slug("idDevice"))
	assert.Equal(t, "porte_d_entree", slug("Porte d'entrée_"))
	assert.Equal(t, "gsm_level", slug("GSMLevel"))
}
