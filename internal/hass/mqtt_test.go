package hass

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrokerURL(t *testing.T) {
	addresses := map[string]string{
		"core-mosquitto":          "tcp://core-mosquitto:1883",
		"core-mosquitto:1884":     "tcp://core-mosquitto:1884",
		"mqtt://broker":           "tcp://broker:1883",
		"mqtts://broker":          "ssl://broker:8883",
		"tcp://192.168.1.10:1883": "tcp://192.168.1.10:1883",
		"ws://broker:9001/mqtt":   "ws://broker:9001/mqtt",
	}

	for address, expected := range addresses {
		normalized, err := brokerURL(address)
		require.NoError(t, err, address)
		assert.Equal(t, expected, normalized, address)
	}
}

func TestBrokerURLRejectsUnusableAddresses(t *testing.T) {
	for _, address := range []string{"", "http://broker", "amqp://broker:5672"} {
		_, err := brokerURL(address)
		assert.ErrorIs(t, err, ErrBrokerAddress, address)
	}
}
