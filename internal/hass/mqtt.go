package hass

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	publishTimeout = 10 * time.Second
	connectTimeout = 15 * time.Second
	quiesce        = 250 * time.Millisecond
)

// ErrBrokerAddress reports a broker address that cannot be used: unlike a
// broker being down, retrying will not help.
var ErrBrokerAddress = errors.New("invalid MQTT broker address")

// MQTTConfig describes how to reach the broker.
type MQTTConfig struct {
	BrokerURL string
	Username  string
	Password  string
	ClientID  string
}

// MQTTPublisher publishes retained messages with QoS 1 and registers a will so
// that Home Assistant sees the entities go unavailable if the bridge dies.
type MQTTPublisher struct {
	client            mqtt.Client
	availabilityTopic string
	online            atomic.Bool
	connections       atomic.Int64
	reconnected       atomic.Bool
}

// Connect dials the broker, declaring availabilityTopic as the will topic and
// replaying the last known availability on every (re)connection.
func Connect(config MQTTConfig, availabilityTopic string, logger *slog.Logger) (*MQTTPublisher, error) {
	broker, err := brokerURL(config.BrokerURL)
	if err != nil {
		return nil, err
	}

	publisher := &MQTTPublisher{availabilityTopic: availabilityTopic}
	options := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(config.ClientID).
		SetUsername(config.Username).
		SetPassword(config.Password).
		SetWill(availabilityTopic, "offline", 1, true).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectTimeout(connectTimeout).
		SetKeepAlive(30 * time.Second).
		SetOnConnectHandler(func(client mqtt.Client) {
			if publisher.connections.Add(1) > 1 {
				// A broker restarted without persistence has forgotten the
				// retained discovery payloads.
				publisher.reconnected.Store(true)
			}
			// The broker published the will when the connection dropped: say
			// again where we stand instead of waiting for the next poll.
			token := client.Publish(availabilityTopic, 1, true, availabilityPayload(publisher.online.Load()))
			token.WaitTimeout(publishTimeout)
			if err := token.Error(); err != nil {
				logger.Warn("cannot republish availability", "error", err)
			}
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			logger.Warn("MQTT connection lost", "error", err)
		})
	if isTLS(broker) {
		options.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	publisher.client = mqtt.NewClient(options)
	token := publisher.client.Connect()
	if !token.WaitTimeout(connectTimeout) {
		publisher.client.Disconnect(0)
		return nil, fmt.Errorf("connect to %s: timeout", broker)
	}
	if err := token.Error(); err != nil {
		publisher.client.Disconnect(0)
		return nil, fmt.Errorf("connect to %s: %w", broker, err)
	}
	return publisher, nil
}

// availabilityPayload is what Home Assistant expects on the availability topic.
func availabilityPayload(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

// Publish sends payload to topic and waits for the broker acknowledgement.
func (p *MQTTPublisher) Publish(topic string, retain bool, payload []byte) error {
	if topic == p.availabilityTopic {
		p.online.Store(string(payload) == "online")
	}
	token := p.client.Publish(topic, 1, retain, payload)
	if !token.WaitTimeout(publishTimeout) {
		return fmt.Errorf("publish to %s: timeout", topic)
	}
	return token.Error()
}

// Reconnected reports, once, that the connection was re-established since the
// last call.
func (p *MQTTPublisher) Reconnected() bool {
	return p.reconnected.Swap(false)
}

// Close says goodbye to the broker.
func (p *MQTTPublisher) Close() {
	p.client.Disconnect(uint(quiesce.Milliseconds()))
}

// brokerURL normalizes a broker address such as "core-mosquitto:1883" into a
// URL paho understands.
func brokerURL(address string) (string, error) {
	if address == "" {
		return "", fmt.Errorf("%w: no broker configured", ErrBrokerAddress)
	}
	if !hasScheme(address) {
		address = "tcp://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrBrokerAddress, address, err)
	}
	if parsed.Port() == "" {
		if isTLS(address) {
			parsed.Host += ":8883"
		} else {
			parsed.Host += ":1883"
		}
	}
	switch parsed.Scheme {
	case "mqtt":
		parsed.Scheme = "tcp"
	case "mqtts", "tls":
		parsed.Scheme = "ssl"
	}
	if !slices.Contains([]string{"tcp", "ssl", "ws", "wss"}, parsed.Scheme) {
		return "", fmt.Errorf("%w: %q is not one of mqtt, mqtts, tcp, ssl, ws or wss", ErrBrokerAddress, parsed.Scheme)
	}
	return parsed.String(), nil
}

func hasScheme(address string) bool {
	parsed, err := url.Parse(address)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func isTLS(address string) bool {
	parsed, err := url.Parse(address)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "mqtts", "ssl", "tls", "wss":
		return true
	default:
		return false
	}
}
