#!/bin/sh
set -eu

CONFIG_PATH="${V2NODE_CONFIG_PATH:-/etc/v2node/config.yml}"
if [ "$CONFIG_PATH" = "/etc/v2node/config.yml" ] && [ ! -f "$CONFIG_PATH" ]; then
	if [ -f /etc/v2node/config.json ]; then
		CONFIG_PATH="/etc/v2node/config.json"
	elif [ -f /etc/v2node/config.yaml ]; then
		CONFIG_PATH="/etc/v2node/config.yaml"
	fi
fi

API_HOST="${V2NODE_API_HOST:-${API_HOST:-}}"
NODE_ID="${V2NODE_NODE_ID:-${NODE_ID:-}}"
API_KEY="${V2NODE_API_KEY:-${API_KEY:-}}"
NODE_CONFIG_DIR="${V2NODE_NODE_CONFIG_DIR:-${V2NODE_CONFIG_DIR:-}}"
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

yaml_escape() {
	printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/\"/\\"/g'
}

load_panel_env_from_config() {
	if [ -z "$API_HOST" ] || [ -z "$NODE_ID" ] || [ -z "$API_KEY" ] || [ -z "$NODE_CONFIG_DIR" ]; then
		if command -v jq >/dev/null 2>&1 && printf '%s' "$CONFIG_PATH" | grep -Eq '\.json$'; then
			API_HOST="${API_HOST:-$(jq -r '.Nodes[0].ApiHost // empty' "$CONFIG_PATH" 2>/dev/null || true)}"
			NODE_ID="${NODE_ID:-$(jq -r '.Nodes[0].NodeID // empty' "$CONFIG_PATH" 2>/dev/null || true)}"
			API_KEY="${API_KEY:-$(jq -r '.Nodes[0].ApiKey // empty' "$CONFIG_PATH" 2>/dev/null || true)}"
			NODE_CONFIG_DIR="${NODE_CONFIG_DIR:-$(jq -r '.Nodes[0].ConfigDir // empty' "$CONFIG_PATH" 2>/dev/null || true)}"
		elif [ -f "$CONFIG_PATH" ]; then
			panel_values="$(awk '
				function trim(s) {
					sub(/^[[:space:]]+/, "", s)
					sub(/[[:space:]]+$/, "", s)
					gsub(/^"/, "", s)
					gsub(/"$/, "", s)
					return s
				}
				/^[[:space:]]*#/ { next }
				/^[^[:space:]-][^:]*:/ {
					name=$0
					sub(/:.*/, "", name)
					section=trim(name)
				}
				section=="panel" {
					if ($0 ~ /^[[:space:]]+url:[[:space:]]*/ && panel_url == "") {
						value=$0
						sub(/^[^:]*:[[:space:]]*/, "", value)
						panel_url=trim(value)
					}
					if ($0 ~ /^[[:space:]]+token:[[:space:]]*/ && panel_token == "") {
						value=$0
						sub(/^[^:]*:[[:space:]]*/, "", value)
						panel_token=trim(value)
					}
					if ($0 ~ /^[[:space:]]+node_id:[[:space:]]*/ && panel_node_id == "") {
						value=$0
						sub(/^[^:]*:[[:space:]]*/, "", value)
						panel_node_id=trim(value)
					}
				}
				section=="kernel" {
					if ($0 ~ /^[[:space:]]+config_dir:[[:space:]]*/ && kernel_config_dir == "") {
						value=$0
						sub(/^[^:]*:[[:space:]]*/, "", value)
						kernel_config_dir=trim(value)
					}
				}
				section=="nodes" {
					if ($0 ~ /^[[:space:]]*-[[:space:]]*node_id:[[:space:]]*/) {
						if (first_node_id == "") {
							value=$0
							sub(/.*node_id:[[:space:]]*/, "", value)
							first_node_id=trim(value)
							in_first_node=1
						} else {
							in_first_node=0
						}
					}
					if ($0 ~ /^[[:space:]]+url:[[:space:]]*/ && node_url == "") {
						value=$0
						sub(/^[^:]*:[[:space:]]*/, "", value)
						node_url=trim(value)
					}
					if ($0 ~ /^[[:space:]]+token:[[:space:]]*/ && node_token == "") {
						value=$0
						sub(/^[^:]*:[[:space:]]*/, "", value)
						node_token=trim(value)
					}
					if (in_first_node && $0 ~ /^[[:space:]]+config_dir:[[:space:]]*/ && first_node_config_dir == "") {
						value=$0
						sub(/^[^:]*:[[:space:]]*/, "", value)
						first_node_config_dir=trim(value)
					}
				}
				END {
					printf "%s|%s|%s|%s|%s|%s|%s|%s\n", panel_url, panel_token, panel_node_id, first_node_id, node_url, node_token, kernel_config_dir, first_node_config_dir
				}
			' "$CONFIG_PATH")"
			panel_url="$(printf '%s' "$panel_values" | cut -d'|' -f1)"
			panel_token="$(printf '%s' "$panel_values" | cut -d'|' -f2)"
			panel_node_id="$(printf '%s' "$panel_values" | cut -d'|' -f3)"
			first_node_id="$(printf '%s' "$panel_values" | cut -d'|' -f4)"
			first_node_url="$(printf '%s' "$panel_values" | cut -d'|' -f5)"
			first_node_token="$(printf '%s' "$panel_values" | cut -d'|' -f6)"
			kernel_config_dir="$(printf '%s' "$panel_values" | cut -d'|' -f7)"
			first_node_config_dir="$(printf '%s' "$panel_values" | cut -d'|' -f8)"
			API_HOST="${API_HOST:-${panel_url:-$first_node_url}}"
			API_KEY="${API_KEY:-${panel_token:-$first_node_token}}"
			NODE_ID="${NODE_ID:-${panel_node_id:-$first_node_id}}"
			NODE_CONFIG_DIR="${NODE_CONFIG_DIR:-${first_node_config_dir:-$kernel_config_dir}}"
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
		echo "v2node: missing API env for env-based cert download; set V2NODE_API_HOST/V2NODE_NODE_ID/V2NODE_API_KEY (or mount a compatible config file)." >&2
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
		cert_base_dir="${NODE_CONFIG_DIR:-/etc/v2node}"
		cert_file="${cert_base_dir%/}/${protocol}${NODE_ID}.cer"
	fi
	if [ -z "$key_file" ]; then
		cert_base_dir="${NODE_CONFIG_DIR:-/etc/v2node}"
		key_file="${cert_base_dir%/}/${protocol}${NODE_ID}.key"
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
	normalized_node_config_dir="${NODE_CONFIG_DIR:-/etc/v2node}"
	node_config_dir_escaped="$(json_escape "$normalized_node_config_dir")"
	node_config_dir_yaml_escaped="$(yaml_escape "$normalized_node_config_dir")"
	runtime_gomemlimit_escaped="$(json_escape "$RUNTIME_GOMEMLIMIT")"
	runtime_gomemlimit_yaml_escaped="$(yaml_escape "$RUNTIME_GOMEMLIMIT")"
	log_level_yaml_escaped="$(yaml_escape "$LOG_LEVEL")"
	core_log_level_yaml_escaped="$(yaml_escape "$CORE_LOG_LEVEL")"
	api_host_yaml_escaped="$(yaml_escape "$API_HOST")"
	api_key_yaml_escaped="$(yaml_escape "$API_KEY")"

	mkdir -p "$(dirname "$CONFIG_PATH")"
	case "$CONFIG_PATH" in
		*.json)
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
			      "Timeout": ${TIMEOUT},
			      "ConfigDir": "${node_config_dir_escaped}"
			    }
			  ]
			}
			EOF
			;;
		*)
			cat >"$CONFIG_PATH" <<-EOF
			panel:
			  url: "${api_host_yaml_escaped}"
			  token: "${api_key_yaml_escaped}"
			  node_id: ${NODE_ID}
			  timeout: ${TIMEOUT}

			kernel:
			  config_dir: "${node_config_dir_yaml_escaped}"
			  log_level: "${core_log_level_yaml_escaped}"

			log:
			  level: "${log_level_yaml_escaped}"
			  output: ""
			  access: "none"

			runtime:
			  gomemlimit: "${runtime_gomemlimit_yaml_escaped}"
			  gogc: ${RUNTIME_GOGC}

			health_port: ${HEALTH_PORT}
			pprof_port: ${PPROF_PORT}
			EOF
			;;
	esac
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
