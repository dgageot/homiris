// Command homiris exposes a Homiris (EPS) alarm system to Home Assistant over
// MQTT discovery. Everything it publishes is read-only: the API refuses to
// arm or disarm without the biometric pairing of the official app.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"homiris/internal/hass"
	"homiris/internal/homiris"
)

const (
	defaultPollInterval = time.Minute
	minPollInterval     = 10 * time.Second
	connectRetryDelay   = 5 * time.Second
	maxBackoff          = 15 * time.Minute
)

var (
	errCredentials = errors.New("set HOMIRIS_LOGIN, HOMIRIS_PASSWORD and HOMIRIS_CLIENT_TOKEN")
	errUsage       = errors.New("usage: homiris [bridge | status | dump]")
)

type options struct {
	homiris  homiris.Config
	mqtt     hass.MQTTConfig
	topics   hass.Topics
	interval time.Duration
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(ctx, os.Args[1:], os.Stdout, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("homiris", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, logger *slog.Logger) error {
	command, err := parseCommand(args)
	if err != nil {
		return err
	}

	options, err := loadOptions()
	if err != nil {
		return err
	}
	client := homiris.New(options.homiris)

	switch command {
	case "status":
		return printStatus(ctx, client, stdout)
	case "dump":
		return dump(ctx, client, stdout)
	default:
		return runBridge(ctx, options, client, logger)
	}
}

func parseCommand(args []string) (string, error) {
	switch len(args) {
	case 0:
		return "bridge", nil
	case 1:
		if command := args[0]; command == "bridge" || command == "status" || command == "dump" {
			return command, nil
		}
	}
	return "", errUsage
}

func loadOptions() (options, error) {
	login := os.Getenv("HOMIRIS_LOGIN")
	password := os.Getenv("HOMIRIS_PASSWORD")
	clientToken := os.Getenv("HOMIRIS_CLIENT_TOKEN")
	if login == "" || password == "" || clientToken == "" {
		return options{}, errCredentials
	}

	interval, err := pollInterval(os.Getenv("HOMIRIS_POLL_INTERVAL"))
	if err != nil {
		return options{}, err
	}

	topics := hass.Topics{
		DiscoveryPrefix: cmp.Or(os.Getenv("MQTT_DISCOVERY_PREFIX"), "homeassistant"),
		Prefix:          cmp.Or(os.Getenv("MQTT_TOPIC_PREFIX"), "homiris"),
		NodeID:          cmp.Or(os.Getenv("HOMIRIS_NODE_ID"), "homiris"),
		DeviceName:      cmp.Or(os.Getenv("HOMIRIS_DEVICE_NAME"), "Homiris"),
	}
	if err := checkTopics(topics); err != nil {
		return options{}, err
	}

	return options{
		homiris: homiris.Config{
			BaseURL:     os.Getenv("HOMIRIS_BASE_URL"),
			Login:       login,
			Password:    password,
			ClientToken: clientToken,
			PhoneType:   os.Getenv("HOMIRIS_PHONE_TYPE"),
		},
		mqtt: hass.MQTTConfig{
			BrokerURL: cmp.Or(os.Getenv("MQTT_BROKER"), os.Getenv("MQTT_HOST")),
			Username:  os.Getenv("MQTT_USERNAME"),
			Password:  os.Getenv("MQTT_PASSWORD"),
			ClientID:  cmp.Or(os.Getenv("MQTT_CLIENT_ID"), "homiris-bridge"),
		},
		topics:   topics,
		interval: interval,
	}, nil
}

func pollInterval(value string) (time.Duration, error) {
	if value == "" {
		return defaultPollInterval, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse HOMIRIS_POLL_INTERVAL: %w", err)
	}
	if interval < minPollInterval {
		return 0, fmt.Errorf("HOMIRIS_POLL_INTERVAL must be at least %s", minPollInterval)
	}
	return interval, nil
}

// checkTopics rejects the characters that would reshape the topics or turn them
// into subscription wildcards.
func checkTopics(topics hass.Topics) error {
	named := map[string]string{
		"MQTT_DISCOVERY_PREFIX": topics.DiscoveryPrefix,
		"MQTT_TOPIC_PREFIX":     topics.Prefix,
		"HOMIRIS_NODE_ID":       topics.NodeID,
	}
	for name, value := range named {
		if strings.ContainsAny(value, "/+#") || strings.ContainsFunc(value, unicode.IsControl) || strings.ContainsFunc(value, unicode.IsSpace) {
			return fmt.Errorf("%s must not contain '/', '+', '#', whitespace or control characters", name)
		}
	}
	return nil
}

func printStatus(ctx context.Context, client *homiris.Client, stdout io.Writer) error {
	homepage, err := client.Homepage(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s (%s)\n", homepage.AlarmState(), homepage.SecurityMode())
	return err
}

func dump(ctx context.Context, client *homiris.Client, stdout io.Writer) error {
	homepage, err := client.Homepage(ctx)
	if err != nil {
		return err
	}
	indented, err := homepage.Indent()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(indented))
	return err
}

func runBridge(ctx context.Context, options options, client *homiris.Client, logger *slog.Logger) error {
	publisher, err := connect(ctx, options, logger)
	if err != nil {
		return err
	}
	defer publisher.Close()

	bridge := &hass.Bridge{Topics: options.topics, Publisher: publisher}
	defer func() {
		if err := bridge.PublishAvailability(false); err != nil {
			logger.Warn("goodbye failed", "error", err)
		}
	}()

	logger.Info("bridge started", "interval", options.interval, "state_topic", options.topics.State())
	return poll(ctx, bridge, client, options.interval, logger)
}

// connect keeps trying to reach the broker: it is normal for it to start after
// the add-on, or to be restarted while the bridge runs.
func connect(ctx context.Context, options options, logger *slog.Logger) (*hass.MQTTPublisher, error) {
	for attempt := 0; ; attempt++ {
		publisher, err := hass.Connect(options.mqtt, options.topics.Availability(), logger)
		if err == nil {
			return publisher, nil
		}
		if errors.Is(err, hass.ErrBrokerAddress) {
			return nil, err
		}
		logger.Warn("cannot reach the MQTT broker", "error", err)
		if err := sleep(ctx, backoff(connectRetryDelay, attempt)); err != nil {
			return nil, err
		}
	}
}

func poll(ctx context.Context, bridge *hass.Bridge, client *homiris.Client, interval time.Duration, logger *slog.Logger) error {
	failures := 0
	for {
		wait := interval
		if err := publishOnce(ctx, bridge, client); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			failures++
			wait = backoff(interval, failures-1)
			logger.Error("update failed", "error", err, "retry_in", wait)
			if err := bridge.PublishAvailability(false); err != nil {
				logger.Warn("cannot mark unavailable", "error", err)
			}
		} else {
			failures = 0
		}

		if err := sleep(ctx, wait); err != nil {
			return nil
		}
	}
}

func publishOnce(ctx context.Context, bridge *hass.Bridge, client *homiris.Client) error {
	homepage, err := client.Homepage(ctx)
	if err != nil {
		return err
	}
	return bridge.PublishReadings(hass.Readings(homepage, time.Now()))
}

// backoff doubles the delay after each failure, so that a broken API or wrong
// credentials are not hammered every interval.
func backoff(interval time.Duration, attempt int) time.Duration {
	delay := interval
	for range min(attempt, 10) {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
