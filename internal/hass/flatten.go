// Package hass turns Homiris data into Home Assistant entities published over
// MQTT discovery. Everything it exposes is read-only.
package hass

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// maxStateLength is the longest state Home Assistant accepts.
const maxStateLength = 255

// sensitiveFragments match anywhere in a field name: no legitimate alarm field
// contains them.
var sensitiveFragments = []string{
	"token", "password", "passwd", "pwd", "secret", "session", "iban",
	"mail", "firstname", "lastname", "prenom", "civilite", "birth", "naissance",
	"latitude", "longitude", "avatar", "photo", "iccid", "imei",
}

// sensitiveFields are dropped, with everything below them, when they match a
// field name exactly. They are words that also appear in harmless names, such
// as "telecommande" or "contactType", hence the exact match.
var sensitiveFields = []string{
	"login", "user", "users", "utilisateur", "person", "personne", "people",
	"contact", "contacts", "customer", "client", "clients", "abonne", "abonnement",
	"contract", "contracts", "contrat", "contrats", "billing", "facture", "invoice", "payment",
	"address", "addresses", "adresse", "adresses", "city", "ville", "postal", "code_postal",
	"zip", "country", "pays", "phone", "phone_number", "tel", "telephone", "mobile",
	"sim", "url", "href", "image", "images",
}

// idKeys name an array element in a way that survives a rename in the app.
var idKeys = []string{"id", "idDevice", "idPeripheral", "idProduct", "idZone", "reference", "code"}

// labelKeys hold the name a user would recognize.
var labelKeys = []string{"name", "label", "nom", "libelle"}

// Reading is a single value published to Home Assistant.
type Reading struct {
	Key   string // stable path, used as entity id and as key in the state document
	Label string // human readable path, used as entity name
	Field string // name of the API field it comes from, used to guess its meaning
	Value any
}

// Flatten reduces a JSON payload to its leaf values, keyed by a slugified path
// such as "system_last_state_security_mode". Array elements are keyed by their
// identifier when they have one, by position otherwise, so that entity ids stay
// stable when peripherals are renamed or events scroll by.
func Flatten(value any) map[string]Reading {
	readings := map[string]Reading{}
	flatten(Reading{}, value, readings)
	return readings
}

func flatten(path Reading, value any, readings map[string]Reading) {
	switch value := value.(type) {
	case map[string]any:
		for _, key := range slices.Sorted(maps.Keys(value)) {
			if isSensitive(key) {
				continue
			}
			flatten(descend(path, slug(key), humanize(slug(key)), slug(key)), value[key], readings)
		}
	case []any:
		names := elementNames(value)
		for index, item := range value {
			flatten(descend(path, names[index], elementLabel(item, index), path.Field), item, readings)
		}
	case string:
		if value != "" && len(value) <= maxStateLength {
			add(readings, path, value)
		}
	case bool:
		add(readings, path, value)
	case json.Number:
		add(readings, path, value)
	case float64:
		add(readings, path, value)
	}
}

func descend(path Reading, key, label, field string) Reading {
	return Reading{
		Key:   join(path.Key, key, "_"),
		Label: join(path.Label, label, " "),
		Field: field,
	}
}

func add(readings map[string]Reading, path Reading, value any) {
	if path.Key == "" {
		return
	}
	path.Value = value
	readings[path.Key] = path
}

// elementNames keys the elements of an array by their identifier, falling back
// to positions for the whole array as soon as an identifier is missing or
// duplicated: a reading must never be overwritten by a sibling.
func elementNames(items []any) []string {
	names := make([]string, len(items))
	seen := map[string]bool{}
	for index, item := range items {
		id := elementID(item)
		if id == "" || seen[id] {
			return positions(len(items))
		}
		seen[id] = true
		names[index] = id
	}
	return names
}

func positions(count int) []string {
	names := make([]string, count)
	for index := range names {
		names[index] = strconv.Itoa(index)
	}
	return names
}

func elementID(item any) string {
	object, ok := item.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range idKeys {
		switch value := object[key].(type) {
		case string:
			if id := slug(value); id != "" {
				return id
			}
		case json.Number:
			return slug(value.String())
		}
	}
	return ""
}

func elementLabel(item any, index int) string {
	if object, ok := item.(map[string]any); ok {
		for _, key := range labelKeys {
			if value, ok := object[key].(string); ok && value != "" {
				return value
			}
		}
	}
	return strconv.Itoa(index)
}

// isSensitive reports whether a field holds credentials or personal data, which
// have no place in a retained MQTT message.
func isSensitive(key string) bool {
	slugged := slug(key)
	if slices.Contains(sensitiveFields, slugged) {
		return true
	}
	return slices.ContainsFunc(sensitiveFragments, func(fragment string) bool {
		return strings.Contains(slugged, fragment)
	})
}

func humanize(key string) string {
	return strings.ReplaceAll(key, "_", " ")
}

func join(prefix, part, separator string) string {
	switch {
	case prefix == "":
		return part
	case part == "":
		return prefix
	default:
		return prefix + separator + part
	}
}

// slug turns a camelCase or accented label into a snake_case identifier.
func slug(value string) string {
	runes := []rune(value)
	var builder strings.Builder
	for index, r := range runes {
		if isUpperBoundary(runes, index) {
			builder.WriteRune('_')
		}
		lowered := fold(unicode.ToLower(r))
		if lowered < 128 && (unicode.IsLetter(lowered) || unicode.IsDigit(lowered)) {
			builder.WriteRune(lowered)
		} else {
			builder.WriteRune('_')
		}
	}
	return strings.Trim(collapseUnderscores(builder.String()), "_")
}

// isUpperBoundary reports whether a word starts at index, as in "securityMode"
// or "GSMLevel".
func isUpperBoundary(runes []rune, index int) bool {
	if index == 0 || !unicode.IsUpper(runes[index]) {
		return false
	}
	previous := runes[index-1]
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}
	return unicode.IsUpper(previous) && index+1 < len(runes) && unicode.IsLower(runes[index+1])
}

// accents maps the accented letters found in French labels to ASCII.
var accents = map[rune]rune{
	'à': 'a', 'â': 'a', 'ä': 'a', 'á': 'a', 'ã': 'a', 'å': 'a',
	'ç': 'c', 'ñ': 'n',
	'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i',
	'ò': 'o', 'ó': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u',
	'ý': 'y', 'ÿ': 'y',
}

func fold(r rune) rune {
	if folded, ok := accents[r]; ok {
		return folded
	}
	return r
}

func collapseUnderscores(value string) string {
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return value
}
