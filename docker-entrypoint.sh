#!/bin/sh
set -eu

CONFIG_PATH="${V2NODE_CONFIG_PATH:-/etc/v2node/config.json}"

API_HOST="${V2NODE_API_HOST:-${API_HOST:-}}"
NODE_ID="${V2NODE_NODE_ID:-${NODE_ID:-}}"
API_KEY="${V2NODE_API_KEY:-${API_KEY:-}}"
TIMEOUT_RAW="${V2NODE_TIMEOUT:-${TIMEOUT:-}}"
# Panel API timeout (seconds). Needs to be long enough to download the initial full user list
# for large deployments; subsequent pulls usually hit ETag/304 and return quickly.
TIMEOUT="${TIMEOUT_RAW:-30}"

PPROF_PORT_RAW="${V2NODE_PPROF_PORT:-${PPROF_PORT:-}}"
PPROF_PORT="${PPROF_PORT_RAW:-0}"
HEALTH_PORT_RAW="${V2NODE_HEALTH_PORT:-${HEALTH_PORT:-}}"
HEALTH_PORT="${HEALTH_PORT_RAW:-0}"

LOG_LEVEL="${V2NODE_LOG_LEVEL:-info}"
CORE_LOG_LEVEL="${V2NODE_CORE_LOG_LEVEL:-error}"
RUNTIME_GOMEMLIMIT="${V2NODE_GOMEMLIMIT:-${GOMEMLIMIT:-}}"
RUNTIME_GOGC_RAW="${V2NODE_GOGC:-${GOGC:-}}"
RUNTIME_GOGC="${RUNTIME_GOGC_RAW:-0}"

TLS_CERT_URL="${V2NODE_TLS_CERT_URL:-${V2NODE_CERT_URL:-}}"
TLS_KEY_URL="${V2NODE_TLS_KEY_URL:-${V2NODE_KEY_URL:-}}"

GEOIP_URL="${V2NODE_GEOIP_URL:-https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat}"
GEOSITE_URL="${V2NODE_GEOSITE_URL:-https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat}"
GEO_ASSET_DIR="${V2NODE_GEO_ASSET_DIR:-${XRAY_LOCATION_ASSET:-/etc/v2node}}"

json_escape() {
	printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/\"/\\\"/g'
}

load_panel_env_from_config() {
	if [ -z "$API_HOST" ] || [ -z "$NODE_ID" ] || [ -z "$API_KEY" ]; then
		if command -v jq >/dev/null 2>&1; then
			API_HOST="${API_HOST:-$(jq -r '.Nodes[0].ApiHost // empty' "$CONFIG_PATH" 2>/dev/null || true)}"
			NODE_ID="${NODE_ID:-$(jq -r '.Nodes[0].NodeID // empty' "$CONFIG_PATH" 2>/dev/null || true)}"
			API_KEY="${API_KEY:-$(jq -r '.Nodes[0].ApiKey // empty' "$CONFIG_PATH" 2>/dev/null || true)}"
		fi
	fi
}

download_to_path() {
	url="$1"
	dest="$2"
	perm="$3"

	mkdir -p "$(dirname "$dest")"
	tmp="${dest}.tmp"

	curl -fsSL --connect-timeout 10 --max-time 60 "$url" -o "$tmp"
	chmod "$perm" "$tmp"
	mv -f "$tmp" "$dest"
}

maybe_download_tls_files() {
	if [ -z "$TLS_CERT_URL" ] && [ -z "$TLS_KEY_URL" ]; then
		return 0
	fi
	if [ -z "$TLS_CERT_URL" ] || [ -z "$TLS_KEY_URL" ]; then
		echo "v2node: set both V2NODE_TLS_CERT_URL and V2NODE_TLS_KEY_URL (or V2NODE_CERT_URL and V2NODE_KEY_URL)." >&2
		exit 2
	fi

	if ! command -v curl >/dev/null 2>&1; then
		echo "v2node: curl is required for env-based cert download." >&2
		exit 2
	fi
	if ! command -v jq >/dev/null 2>&1; then
		echo "v2node: jq is required for env-based cert download." >&2
		exit 2
	fi

	load_panel_env_from_config
	if [ -z "$API_HOST" ] || [ -z "$NODE_ID" ] || [ -z "$API_KEY" ]; then
		echo "v2node: missing API env for env-based cert download; set V2NODE_API_HOST/V2NODE_NODE_ID/V2NODE_API_KEY (or mount a config.json with Nodes[0])." >&2
		exit 2
	fi

	api_base="${API_HOST%/}"
	node_json="$(curl -fsSL --connect-timeout 10 --max-time 60 --get "${api_base}/api/v2/server/config" \
		--data-urlencode "node_type=v2node" \
		--data-urlencode "node_id=${NODE_ID}" \
		--data-urlencode "token=${API_KEY}")"

	protocol="$(printf '%s' "$node_json" | jq -r '.protocol // empty')"
	cert_mode="$(printf '%s' "$node_json" | jq -r '.tls_settings.cert_mode // empty')"
	cert_file="$(printf '%s' "$node_json" | jq -r '.tls_settings.cert_file // empty')"
	key_file="$(printf '%s' "$node_json" | jq -r '.tls_settings.key_file // empty')"

	if [ -z "$protocol" ]; then
		echo "v2node: unable to detect node protocol from panel API response." >&2
		exit 2
	fi
	if [ "$cert_mode" != "file" ] && [ -z "$cert_file$key_file" ]; then
		return 0
	fi
	if [ -z "$cert_file" ]; then
		cert_file="/etc/v2node/${protocol}${NODE_ID}.cer"
	fi
	if [ -z "$key_file" ]; then
		key_file="/etc/v2node/${protocol}${NODE_ID}.key"
	fi

	download_to_path "$TLS_CERT_URL" "$cert_file" 0644
	download_to_path "$TLS_KEY_URL" "$key_file" 0600
}

maybe_download_geo_assets() {
	if [ "${V2NODE_SKIP_GEO_ASSETS:-}" = "1" ]; then
		return 0
	fi

	# Ensure xray-core can find geosite.dat/geoip.dat
	if [ -z "${XRAY_LOCATION_ASSET:-}" ]; then
		export XRAY_LOCATION_ASSET="$GEO_ASSET_DIR"
	fi

	geoip_path="${GEO_ASSET_DIR%/}/geoip.dat"
	geosite_path="${GEO_ASSET_DIR%/}/geosite.dat"

	[ -s "$geoip_path" ] && [ -s "$geosite_path" ] && return 0

	if ! command -v curl >/dev/null 2>&1; then
		echo "v2node: curl is required for geoip/geosite download (set V2NODE_SKIP_GEO_ASSETS=1 to skip)." >&2
		return 0
	fi

	if [ ! -s "$geoip_path" ]; then
		echo "v2node: geoip.dat not found, downloading to ${geoip_path} ..." >&2
		download_to_path "$GEOIP_URL" "$geoip_path" 0644 || echo "v2node: failed to download geoip.dat; geoip:* routing rules may fail." >&2
	fi

	if [ ! -s "$geosite_path" ]; then
		echo "v2node: geosite.dat not found, downloading to ${geosite_path} ..." >&2
		download_to_path "$GEOSITE_URL" "$geosite_path" 0644 || echo "v2node: failed to download geosite.dat; geosite:* routing rules may fail." >&2
	fi
}

generate_config_from_env() {
	api_host_escaped="$(json_escape "$API_HOST")"
	api_key_escaped="$(json_escape "$API_KEY")"
	runtime_gomemlimit_escaped="$(json_escape "$RUNTIME_GOMEMLIMIT")"

	mkdir -p "$(dirname "$CONFIG_PATH")"
	cat >"$CONFIG_PATH" <<-EOF
	{
	  "PprofPort": ${PPROF_PORT},
	  "HealthPort": ${HEALTH_PORT},
	  "Log": {
	    "Level": "${LOG_LEVEL}",
	    "CoreLevel": "${CORE_LOG_LEVEL}",
	    "Output": "",
	    "Access": "none"
	  },
	  "Runtime": {
	    "GoMemLimit": "${runtime_gomemlimit_escaped}",
	    "GOGC": ${RUNTIME_GOGC}
	  },
	  "Nodes": [
	    {
	      "ApiHost": "${api_host_escaped}",
	      "NodeID": ${NODE_ID},
	      "ApiKey": "${api_key_escaped}",
	      "Timeout": ${TIMEOUT}
	    }
	  ]
	}
	EOF
}

ensure_config_for_server() {
	if [ -n "$API_HOST" ] || [ -n "$NODE_ID" ] || [ -n "$API_KEY" ]; then
		if [ -z "$API_HOST" ] || [ -z "$NODE_ID" ] || [ -z "$API_KEY" ]; then
			echo "v2node: missing required env vars; set V2NODE_API_HOST/V2NODE_NODE_ID/V2NODE_API_KEY (or API_HOST/NODE_ID/API_KEY)." >&2
			exit 2
		fi
		case "$NODE_ID" in
			*[!0-9]*|'')
				echo "v2node: NODE_ID must be an integer." >&2
				exit 2
				;;
		esac
		case "$TIMEOUT" in
			*[!0-9]*|'')
				echo "v2node: TIMEOUT must be an integer." >&2
				exit 2
				;;
		esac
		case "$PPROF_PORT" in
			*[!0-9]*|'')
				echo "v2node: PPROF_PORT must be an integer." >&2
				exit 2
				;;
		esac
		case "$HEALTH_PORT" in
			*[!0-9]*|'')
				echo "v2node: HEALTH_PORT must be an integer." >&2
				exit 2
				;;
		esac
		if ! printf '%s' "$RUNTIME_GOGC" | grep -Eq '^-?[0-9]+$'; then
			echo "v2node: GOGC must be an integer." >&2
			exit 2
		fi
		generate_config_from_env
	fi

	if [ ! -f "$CONFIG_PATH" ]; then
		echo "v2node: config file not found at $CONFIG_PATH." >&2
		echo "  - mount a config file, or" >&2
		echo "  - set V2NODE_API_HOST/V2NODE_NODE_ID/V2NODE_API_KEY (and optional V2NODE_TIMEOUT) to generate one." >&2
		exit 2
	fi
}

if [ "$#" -eq 0 ]; then
	set -- v2node server
fi

if [ "$1" = "server" ]; then
	set -- v2node "$@"
fi

if [ "$1" = "v2node" ] && [ "${2:-}" = "server" ]; then
	ensure_config_for_server
	maybe_download_tls_files
	maybe_download_geo_assets

	has_config_flag=0
	for arg in "$@"; do
		case "$arg" in
			--config|-c|--config=*|-c=*)
				has_config_flag=1
				break
				;;
		esac
	done
	if [ "$has_config_flag" -eq 0 ]; then
		set -- "$@" --config "$CONFIG_PATH"
	fi
fi

exec "$@"
