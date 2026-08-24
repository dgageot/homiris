package homiris

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHomepageAlarmState(t *testing.T) {
	modes := map[string]string{
		"ARRET":           StateDisarmed,
		"arret":           StateDisarmed,
		"HORS SERVICE":    StateDisarmed,
		"PARTIEL":         StateArmedNight,
		"PARTIEL 2":       StateArmedNight,
		"TOTAL":           StateArmedAway,
		"TOTALE":          StateArmedAway,
		"ALARME":          StateTriggered,
		"PRESENCE":        StateArmedHome,
		"PERIMETRIQUE":    StateArmedHome,
		"EN_COURS":        StateArming,
		"MARCHE EN COURS": StateArming,
		"WHATEVER":        StateUnknown,
		"":                StateUnknown,
	}

	for mode, expected := range modes {
		homepage := Homepage{"systemLastState": map[string]any{"securityMode": mode}}
		assert.Equal(t, expected, homepage.AlarmState(), "mode %q", mode)
	}
}

func TestHomepageSecurityModeFoundAtAnyDepth(t *testing.T) {
	var homepage Homepage
	require.NoError(t, json.Unmarshal([]byte(`{"data":{"systems":[{"securityMode":"TOTAL"}]}}`), &homepage))

	assert.Equal(t, "TOTAL", homepage.SecurityMode())
	assert.Equal(t, StateArmedAway, homepage.AlarmState())
}

func TestHomepageIndent(t *testing.T) {
	indented, err := Homepage{"a": "b"}.Indent()

	require.NoError(t, err)
	assert.Equal(t, "{\n  \"a\": \"b\"\n}", string(indented))
}
