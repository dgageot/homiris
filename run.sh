#!/usr/bin/with-contenv bashio
# Home Assistant add-on entrypoint: turns add-on options into the environment
# variables the bridge expects.
set -eu
set -o pipefail

HOMIRIS_LOGIN="$(bashio::config 'login')"
HOMIRIS_PASSWORD="$(bashio::config 'password')"
HOMIRIS_CLIENT_TOKEN="$(bashio::config 'client_token')"
HOMIRIS_POLL_INTERVAL="$(bashio::config 'poll_interval')s"
export HOMIRIS_LOGIN HOMIRIS_PASSWORD HOMIRIS_CLIENT_TOKEN HOMIRIS_POLL_INTERVAL

for option in phone_type device_name node_id base_url; do
  if bashio::config.has_value "${option}"; then
    value="$(bashio::config "${option}")"
    name="HOMIRIS_$(bashio::string.upper "${option}")"
    export "${name}=${value}"
  fi
done
for option in discovery_prefix topic_prefix; do
  if bashio::config.has_value "${option}"; then
    value="$(bashio::config "${option}")"
    name="MQTT_$(bashio::string.upper "${option}")"
    export "${name}=${value}"
  fi
done

bashio::services.available mqtt || bashio::exit.nok "the MQTT service is required"
scheme="mqtt"
if bashio::var.true "$(bashio::services mqtt 'ssl')"; then
  scheme="mqtts"
fi
host="$(bashio::services mqtt 'host')"
port="$(bashio::services mqtt 'port')"
MQTT_BROKER="${scheme}://${host}:${port}"
export MQTT_BROKER

# An anonymous broker reports no credentials at all.
for credential in username password; do
  value="$(bashio::services mqtt "${credential}")"
  if bashio::var.has_value "${value}"; then
    export "MQTT_$(bashio::string.upper "${credential}")=${value}"
  fi
done

exec /usr/bin/homiris bridge
