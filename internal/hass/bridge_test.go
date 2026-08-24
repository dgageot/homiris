package hass

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"homiris/internal/homiris"
)

type message struct {
	topic   string
	retain  bool
	payload string
}

type fakePublisher struct {
	messages    []message
	err         error
	reconnected bool
}

func (p *fakePublisher) Publish(topic string, retain bool, payload []byte) error {
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, message{topic: topic, retain: retain, payload: string(payload)})
	return nil
}

func (p *fakePublisher) Reconnected() bool {
	reconnected := p.reconnected
	p.reconnected = false
	return reconnected
}

func (p *fakePublisher) topics() []string {
	topics := make([]string, 0, len(p.messages))
	for _, published := range p.messages {
		topics = append(topics, published.topic)
	}
	return topics
}

func TestReadings(t *testing.T) {
	homepage := homiris.Homepage{
		"systemLastState": map[string]any{"securityMode": "TOTAL", "gsmLevel": json.Number("4")},
		"idSession":       "session-id",
	}

	readings := Readings(homepage, time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC))

	assert.Equal(t, map[string]any{
		"alarm_state":                     homiris.StateArmedAway,
		"security_mode":                   "TOTAL",
		"armed":                           true,
		"triggered":                       false,
		"last_updated":                    "2026-08-24T10:00:00Z",
		"system_last_state_security_mode": "TOTAL",
		"system_last_state_gsm_level":     json.Number("4"),
	}, stateDocument(readings))
	assert.Equal(t, "gsm_level", readings["system_last_state_gsm_level"].Field)
}

func TestReadingsWhenModeIsUnknown(t *testing.T) {
	readings := Readings(homiris.Homepage{}, time.Now())

	assert.Equal(t, homiris.StateUnknown, readings[KeyAlarmState].Value)
	assert.Equal(t, homiris.StateUnknown, readings[KeySecurityMode].Value)
	assert.Equal(t, false, readings[KeyArmed].Value)
}

func TestBridgeAnnouncesEntitiesOnce(t *testing.T) {
	publisher := &fakePublisher{}
	bridge := &Bridge{Topics: testTopics(), Publisher: publisher}

	require.NoError(t, bridge.PublishReadings(map[string]Reading{"armed": reading("armed", "armed", true)}))
	require.NoError(t, bridge.PublishReadings(map[string]Reading{"armed": reading("armed", "armed", false)}))

	assert.Equal(t, []string{
		"homeassistant/binary_sensor/homiris/armed/config",
		"homiris/homiris/state",
		"homiris/homiris/status",
		"homiris/homiris/state",
		"homiris/homiris/status",
	}, publisher.topics())
	for _, published := range publisher.messages {
		assert.True(t, published.retain, published.topic)
	}
	assert.JSONEq(t, `{"armed":false}`, publisher.messages[3].payload)
	assert.Equal(t, "online", publisher.messages[4].payload)
}

func TestBridgeAnnouncesNewReadings(t *testing.T) {
	publisher := &fakePublisher{}
	bridge := &Bridge{Topics: testTopics(), Publisher: publisher}

	require.NoError(t, bridge.PublishReadings(map[string]Reading{"armed": reading("armed", "armed", true)}))
	require.NoError(t, bridge.PublishReadings(map[string]Reading{
		"armed":         reading("armed", "armed", true),
		"security_mode": reading("security_mode", "security_mode", "TOTAL"),
	}))

	assert.Contains(t, publisher.topics(), "homeassistant/sensor/homiris/security_mode/config")
}

// Readings come and go with the peripherals and the events reported by the API:
// entities that disappear must not linger in Home Assistant.
func TestBridgeRemovesReadingsThatDisappeared(t *testing.T) {
	publisher := &fakePublisher{}
	bridge := &Bridge{Topics: testTopics(), Publisher: publisher}
	require.NoError(t, bridge.PublishReadings(map[string]Reading{
		"armed":               reading("armed", "armed", true),
		"last_events_0_label": reading("last_events_0_label", "label", "Mise en marche"),
	}))
	publisher.messages = nil

	require.NoError(t, bridge.PublishReadings(map[string]Reading{"armed": reading("armed", "armed", true)}))

	assert.Contains(t, publisher.messages, message{
		topic:  "homeassistant/sensor/homiris/last_events_0_label/config",
		retain: true,
	})
	assert.NotContains(t, publisher.topics(), "homeassistant/binary_sensor/homiris/armed/config")
}

// A field that switches from a number to a boolean changes component: the stale
// configuration has to be cleared, not left behind.
func TestBridgeRepublishesWhenAReadingChangesType(t *testing.T) {
	publisher := &fakePublisher{}
	bridge := &Bridge{Topics: testTopics(), Publisher: publisher}
	require.NoError(t, bridge.PublishReadings(map[string]Reading{"fault": reading("fault", "fault", "NOK")}))
	publisher.messages = nil

	require.NoError(t, bridge.PublishReadings(map[string]Reading{"fault": reading("fault", "fault", true)}))

	assert.Equal(t, []string{
		"homeassistant/sensor/homiris/fault/config",
		"homeassistant/binary_sensor/homiris/fault/config",
		"homiris/homiris/state",
		"homiris/homiris/status",
	}, publisher.topics())
	assert.Empty(t, publisher.messages[0].payload)
}

// A broker that restarted without persistence has lost the retained discovery
// payloads: they have to be sent again.
func TestBridgeAnnouncesAgainAfterAReconnection(t *testing.T) {
	publisher := &fakePublisher{}
	bridge := &Bridge{Topics: testTopics(), Publisher: publisher}
	readings := map[string]Reading{"armed": reading("armed", "armed", true)}
	require.NoError(t, bridge.PublishReadings(readings))
	publisher.messages = nil
	publisher.reconnected = true

	require.NoError(t, bridge.PublishReadings(readings))

	assert.Contains(t, publisher.topics(), "homeassistant/binary_sensor/homiris/armed/config")
}

func TestBridgePublishesUnavailability(t *testing.T) {
	publisher := &fakePublisher{}
	bridge := &Bridge{Topics: testTopics(), Publisher: publisher}

	require.NoError(t, bridge.PublishAvailability(false))

	assert.Equal(t, []message{{topic: "homiris/homiris/status", retain: true, payload: "offline"}}, publisher.messages)
}

func TestBridgeReportsPublishError(t *testing.T) {
	publisher := &fakePublisher{err: assert.AnError}
	bridge := &Bridge{Topics: testTopics(), Publisher: publisher}

	err := bridge.PublishReadings(map[string]Reading{"armed": reading("armed", "armed", true)})

	require.Error(t, err)
	assert.ErrorContains(t, err, "publish discovery for armed")
}
