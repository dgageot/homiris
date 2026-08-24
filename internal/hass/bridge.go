package hass

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"homiris/internal/homiris"
)

// Publisher publishes a payload on an MQTT topic.
type Publisher interface {
	Publish(topic string, retain bool, payload []byte) error
}

// Bridge mirrors the Homiris system into Home Assistant: a discovery payload per
// entity, then a single state document holding every value. It is meant to be
// driven by one goroutine.
type Bridge struct {
	Topics    Topics
	Publisher Publisher

	announced map[string]announcement
}

// announcement remembers what was published for an entity, so that a reading
// which changes type or meaning gets a fresh configuration and an entity which
// disappears gets removed instead of lingering as a retained message.
type announcement struct {
	component   string
	fingerprint string
}

// Readings turns a homepage payload into the values published to Home
// Assistant: every leaf of the API response plus a few computed ones, which win
// in the unlikely case the API uses one of their names.
func Readings(homepage homiris.Homepage, now time.Time) map[string]Reading {
	readings := Flatten(map[string]any(homepage))
	state := homepage.AlarmState()
	computed := map[string]any{
		KeyAlarmState:   state,
		KeySecurityMode: cmp.Or(homepage.SecurityMode(), homiris.StateUnknown),
		KeyArmed:        state != homiris.StateDisarmed && state != homiris.StateUnknown,
		KeyTriggered:    state == homiris.StateTriggered,
		KeyLastUpdated:  now.UTC().Format(time.RFC3339),
	}
	for key, value := range computed {
		readings[key] = Reading{Key: key, Field: key, Value: value}
	}
	return readings
}

// PublishReadings announces new or changed entities, removes the ones that
// disappeared, then publishes the state document and marks the device available.
func (b *Bridge) PublishReadings(readings map[string]Reading) error {
	if err := b.announce(readings); err != nil {
		return err
	}

	payload, err := json.Marshal(stateDocument(readings))
	if err != nil {
		return err
	}
	if err := b.Publisher.Publish(b.Topics.State(), true, payload); err != nil {
		return fmt.Errorf("publish state: %w", err)
	}
	return b.PublishAvailability(true)
}

// PublishAvailability marks the device online or offline.
func (b *Bridge) PublishAvailability(online bool) error {
	if err := b.Publisher.Publish(b.Topics.Availability(), true, []byte(availabilityPayload(online))); err != nil {
		return fmt.Errorf("publish availability: %w", err)
	}
	return nil
}

// stateDocument is the single JSON document every entity reads its value from.
func stateDocument(readings map[string]Reading) map[string]any {
	state := make(map[string]any, len(readings))
	for key, reading := range readings {
		state[key] = reading.Value
	}
	return state
}

// reconnecter is implemented by publishers that know when the connection was
// re-established: the broker may have lost the retained messages meanwhile, so
// everything has to be announced again.
type reconnecter interface {
	Reconnected() bool
}

func (b *Bridge) announce(readings map[string]Reading) error {
	if publisher, ok := b.Publisher.(reconnecter); ok && publisher.Reconnected() {
		b.announced = nil
	}
	if b.announced == nil {
		b.announced = map[string]announcement{}
	}

	for _, entity := range b.Topics.Entities(readings) {
		fingerprint, err := fingerprint(entity)
		if err != nil {
			return err
		}
		previous, known := b.announced[entity.ObjectID]
		if known && previous.fingerprint == fingerprint {
			continue
		}
		if known && previous.component != entity.Component {
			if err := b.remove(entity.ObjectID, previous.component); err != nil {
				return err
			}
		}
		payload, err := json.Marshal(entity.Config)
		if err != nil {
			return err
		}
		if err := b.Publisher.Publish(b.Topics.Discovery(entity), true, payload); err != nil {
			return fmt.Errorf("publish discovery for %s: %w", entity.ObjectID, err)
		}
		b.announced[entity.ObjectID] = announcement{component: entity.Component, fingerprint: fingerprint}
	}

	return b.removeStale(readings)
}

func (b *Bridge) removeStale(readings map[string]Reading) error {
	for _, objectID := range slices.Sorted(maps.Keys(b.announced)) {
		if _, ok := readings[objectID]; ok {
			continue
		}
		if err := b.remove(objectID, b.announced[objectID].component); err != nil {
			return err
		}
		delete(b.announced, objectID)
	}
	return nil
}

// remove deletes an entity from Home Assistant by clearing its retained
// discovery payload.
func (b *Bridge) remove(objectID, component string) error {
	topic := b.Topics.Discovery(Entity{Component: component, ObjectID: objectID})
	if err := b.Publisher.Publish(topic, true, nil); err != nil {
		return fmt.Errorf("remove discovery for %s: %w", objectID, err)
	}
	return nil
}
