package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"homiris/internal/hass"
	"homiris/internal/homiris"
)

func homirisServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/smartphone/production/1.0.0/connect":
			_, _ = w.Write([]byte(`{"idSession":"session-id"}`))
		case "/smartphone/production/1.0.0/homepage/session-id":
			_, _ = w.Write([]byte(`{"systemLastState":{"securityMode":"TOTAL"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func setCredentials(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("HOMIRIS_LOGIN", "123456")
	t.Setenv("HOMIRIS_PASSWORD", "secret")
	t.Setenv("HOMIRIS_CLIENT_TOKEN", "client-token")
	t.Setenv("HOMIRIS_BASE_URL", baseURL)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestParseCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"bridge"}} {
		command, err := parseCommand(args)
		require.NoError(t, err)
		assert.Equal(t, "bridge", command)
	}

	for _, command := range []string{"status", "dump"} {
		parsed, err := parseCommand([]string{command})
		require.NoError(t, err)
		assert.Equal(t, command, parsed)
	}

	_, err := parseCommand([]string{"disarm"})
	assert.ErrorIs(t, err, errUsage)

	_, err = parseCommand([]string{"status", "--yes"})
	assert.ErrorIs(t, err, errUsage)
}

func TestPollInterval(t *testing.T) {
	interval, err := pollInterval("")
	require.NoError(t, err)
	assert.Equal(t, defaultPollInterval, interval)

	interval, err = pollInterval("30s")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, interval)

	_, err = pollInterval("nope")
	assert.ErrorContains(t, err, "parse HOMIRIS_POLL_INTERVAL")

	_, err = pollInterval("1s")
	assert.ErrorContains(t, err, "at least 10s")
}

func TestRunStatus(t *testing.T) {
	setCredentials(t, homirisServer(t).URL)
	var stdout bytes.Buffer

	require.NoError(t, run(t.Context(), []string{"status"}, &stdout, discardLogger()))

	assert.Equal(t, "armed_away (TOTAL)\n", stdout.String())
}

func TestRunDump(t *testing.T) {
	setCredentials(t, homirisServer(t).URL)
	var stdout bytes.Buffer

	require.NoError(t, run(t.Context(), []string{"dump"}, &stdout, discardLogger()))

	assert.JSONEq(t, `{"systemLastState":{"securityMode":"TOTAL"}}`, stdout.String())
}

func TestRunRequiresCredentials(t *testing.T) {
	t.Setenv("HOMIRIS_LOGIN", "")
	t.Setenv("HOMIRIS_PASSWORD", "")
	t.Setenv("HOMIRIS_CLIENT_TOKEN", "")

	err := run(t.Context(), []string{"status"}, io.Discard, discardLogger())

	assert.ErrorIs(t, err, errCredentials)
}

func TestLoadOptionsDefaults(t *testing.T) {
	setCredentials(t, "https://example.com")
	t.Setenv("MQTT_HOST", "core-mosquitto")

	options, err := loadOptions()

	require.NoError(t, err)
	assert.Equal(t, defaultPollInterval, options.interval)
	assert.Equal(t, "homeassistant", options.topics.DiscoveryPrefix)
	assert.Equal(t, "homiris/homiris/state", options.topics.State())
	assert.Equal(t, "core-mosquitto", options.mqtt.BrokerURL)
	assert.Equal(t, "homiris-bridge", options.mqtt.ClientID)
}

func TestLoadOptionsOverrides(t *testing.T) {
	setCredentials(t, "https://example.com")
	t.Setenv("MQTT_BROKER", "mqtts://broker:8883")
	t.Setenv("MQTT_TOPIC_PREFIX", "alarm")
	t.Setenv("MQTT_DISCOVERY_PREFIX", "ha")
	t.Setenv("HOMIRIS_NODE_ID", "home")
	t.Setenv("HOMIRIS_DEVICE_NAME", "Alarme")
	t.Setenv("HOMIRIS_POLL_INTERVAL", "15s")

	options, err := loadOptions()

	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, options.interval)
	assert.Equal(t, "alarm/home/state", options.topics.State())
	assert.Equal(t, "ha", options.topics.DiscoveryPrefix)
	assert.Equal(t, "Alarme", options.topics.DeviceName)
	assert.Equal(t, "mqtts://broker:8883", options.mqtt.BrokerURL)
}

// fakePublisher records topics and lets the test stop the loop after a given
// number of messages.
type fakePublisher struct {
	topics    []string
	stopAfter int
	stop      func()
}

func (p *fakePublisher) Publish(topic string, _ bool, _ []byte) error {
	p.topics = append(p.topics, topic)
	if len(p.topics) >= p.stopAfter {
		p.stop()
	}
	return nil
}

func TestPollPublishesEverythingThenStops(t *testing.T) {
	server := homirisServer(t)
	client := homiris.New(homiris.Config{
		BaseURL:     server.URL,
		Login:       "123456",
		Password:    "secret",
		ClientToken: "client-token",
		HTTPClient:  server.Client(),
	})
	topics := hass.Topics{DiscoveryPrefix: "homeassistant", Prefix: "homiris", NodeID: "homiris", DeviceName: "Homiris"}
	ctx, cancel := context.WithCancel(t.Context())
	// Five entities plus the state and the availability messages.
	publisher := &fakePublisher{stopAfter: 7, stop: cancel}
	bridge := &hass.Bridge{Topics: topics, Publisher: publisher}

	require.NoError(t, poll(ctx, bridge, client, time.Hour, discardLogger()))

	assert.Contains(t, publisher.topics, "homeassistant/sensor/homiris/alarm_state/config")
	assert.Contains(t, publisher.topics, "homeassistant/binary_sensor/homiris/armed/config")
	assert.Contains(t, publisher.topics, "homeassistant/sensor/homiris/security_mode/config")
	assert.Contains(t, publisher.topics, "homiris/homiris/state")
	assert.Equal(t, "homiris/homiris/status", publisher.topics[len(publisher.topics)-1])
}

func TestPollMarksUnavailableOnAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := homiris.New(homiris.Config{
		BaseURL:     server.URL,
		Login:       "123456",
		Password:    "secret",
		ClientToken: "client-token",
		HTTPClient:  server.Client(),
	})
	ctx, cancel := context.WithCancel(t.Context())
	publisher := &fakePublisher{stopAfter: 1, stop: cancel}
	bridge := &hass.Bridge{Topics: hass.Topics{DiscoveryPrefix: "homeassistant", Prefix: "homiris", NodeID: "homiris"}, Publisher: publisher}

	require.NoError(t, poll(ctx, bridge, client, time.Hour, discardLogger()))

	assert.Equal(t, []string{"homiris/homiris/status"}, publisher.topics)
}

func TestBackoffDoublesAndIsCapped(t *testing.T) {
	assert.Equal(t, time.Minute, backoff(time.Minute, 0))
	assert.Equal(t, 2*time.Minute, backoff(time.Minute, 1))
	assert.Equal(t, 8*time.Minute, backoff(time.Minute, 3))
	assert.Equal(t, maxBackoff, backoff(time.Minute, 10))
}

func TestLoadOptionsRejectsWildcardTopics(t *testing.T) {
	setCredentials(t, "https://example.com")
	t.Setenv("HOMIRIS_NODE_ID", "home/#")

	_, err := loadOptions()

	require.Error(t, err)
	assert.ErrorContains(t, err, "HOMIRIS_NODE_ID")
}
