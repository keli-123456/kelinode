#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)
V2NODE_INSTALL_DIR="/usr/local/v2node"
V2NODE_CONFIG_DIR="/etc/v2node"
V2NODE_CONFIG_FILE="${V2NODE_CONFIG_DIR}/config.yml"
V2NODE_VERSION_FILE="${V2NODE_INSTALL_DIR}/.installed_version"
INSTALL_LOCK_DIR="/tmp/v2node-install.lock"

# check root
[[ $EUID -ne 0 ]] && echo -e "${red}错误：${plain} 必须使用root用户运行此脚本！\n" && exit 1

# check os
if [[ -f /etc/redhat-release ]]; then
    release="centos"
elif cat /etc/issue | grep -Eqi "alpine"; then
    release="alpine"
elif cat /etc/issue | grep -Eqi "debian"; then
    release="debian"
elif cat /etc/issue | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /etc/issue | grep -Eqi "centos|red hat|redhat|rocky|alma|oracle linux"; then
    release="centos"
elif cat /proc/version | grep -Eqi "debian"; then
    release="debian"
elif cat /proc/version | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /proc/version | grep -Eqi "centos|red hat|redhat|rocky|alma|oracle linux"; then
    release="centos"
elif cat /proc/version | grep -Eqi "arch"; then
    release="arch"
else
    echo -e "${red}未检测到系统版本，请联系脚本作者！${plain}\n" && exit 1
fi

########################
# 参数解析
########################
VERSION_ARG=""
API_HOST_ARG=""
NODE_ID_ARG=""
API_KEY_ARG=""
MACHINE_URL_ARG=""
MACHINE_ID_ARG=""
MACHINE_TOKEN_ARG=""
MACHINE_NAME_ARG=""
MACHINE_REPLACE_ID_ARG="false"

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --api-host)
                API_HOST_ARG="$2"; shift 2 ;;
            --node-id)
                NODE_ID_ARG="$2"; shift 2 ;;
            --api-key)
                API_KEY_ARG="$2"; shift 2 ;;
            --machine-url)
                MACHINE_URL_ARG="$2"; shift 2 ;;
            --machine-id)
                MACHINE_ID_ARG="$2"; shift 2 ;;
            --machine-token)
                MACHINE_TOKEN_ARG="$2"; shift 2 ;;
            --machine-name)
                MACHINE_NAME_ARG="$2"; shift 2 ;;
            --replace-machine-id)
                MACHINE_REPLACE_ID_ARG="true"; shift ;;
            -h|--help)
                echo "用法: $0 [版本号] [--api-host URL] [--node-id ID] [--api-key KEY]"
                echo "     $0 [版本号] --machine-url URL --machine-id ID --machine-token TOKEN [--machine-name NAME] [--replace-machine-id]"
                exit 0 ;;
            --*)
                echo "未知参数: $1"; exit 1 ;;
            *)
                # 兼容第一个位置参数作为版本号
                if [[ -z "$VERSION_ARG" ]]; then
                    VERSION_ARG="$1"; shift
                else
                    shift
                fi ;;
        esac
    done
}

has_machine_args() {
    [[ -n "$MACHINE_URL_ARG" || -n "$MACHINE_ID_ARG" || -n "$MACHINE_TOKEN_ARG" || -n "$MACHINE_NAME_ARG" ]]
}

validate_args() {
    if has_machine_args; then
        if [[ -n "$API_HOST_ARG" || -n "$NODE_ID_ARG" || -n "$API_KEY_ARG" ]]; then
            echo -e "${red}不能同时使用单节点参数和 machine 参数${plain}"
            exit 1
        fi
        if [[ -z "$MACHINE_URL_ARG" || -z "$MACHINE_ID_ARG" || -z "$MACHINE_TOKEN_ARG" ]]; then
            echo -e "${red}machine 模式需要 --machine-url、--machine-id、--machine-token${plain}"
            exit 1
        fi
        if ! [[ "$MACHINE_ID_ARG" =~ ^[0-9]+$ ]] || [[ "$MACHINE_ID_ARG" -le 0 ]]; then
            echo -e "${red}--machine-id 必须是正整数${plain}"
            exit 1
        fi
    fi
}

acquire_install_lock() {
    local waited=0
    local max_wait=120

    while ! mkdir "$INSTALL_LOCK_DIR" 2>/dev/null; do
        if [[ $waited -ge $max_wait ]]; then
            echo -e "${red}另一个 v2node 安装任务仍在运行，请稍后重试${plain}"
            exit 1
        fi
        echo -e "${yellow}检测到另一个 v2node 安装任务，等待中... (${waited}/${max_wait}秒)${plain}"
        sleep 2
        waited=$((waited + 2))
    done

    trap 'rmdir "$INSTALL_LOCK_DIR" 2>/dev/null || true' EXIT
}

arch=$(uname -m)

if [[ $arch == "x86_64" || $arch == "x64" || $arch == "amd64" ]]; then
    arch="64"
elif [[ $arch == "aarch64" || $arch == "arm64" ]]; then
    arch="arm64-v8a"
elif [[ $arch == "s390x" ]]; then
    arch="s390x"
else
    arch="64"
    echo -e "${red}检测架构失败，使用默认架构: ${arch}${plain}"
fi

if [ "$(getconf WORD_BIT)" != '32' ] && [ "$(getconf LONG_BIT)" != '64' ] ; then
    echo "本软件不支持 32 位系统(x86)，请使用 64 位系统(x86_64)，如果检测有误，请联系作者"
    exit 2
fi

# os version
if [[ -f /etc/os-release ]]; then
    os_version=$(awk -F'[= ."]' '/VERSION_ID/{print $3}' /etc/os-release)
fi
if [[ -z "$os_version" && -f /etc/lsb-release ]]; then
    os_version=$(awk -F'[= ."]+' '/DISTRIB_RELEASE/{print $2}' /etc/lsb-release)
fi

if [[ x"${release}" == x"centos" ]]; then
    if [[ ${os_version} -le 6 ]]; then
        echo -e "${red}请使用 CentOS 7 或更高版本的系统！${plain}\n" && exit 1
    fi
    if [[ ${os_version} -eq 7 ]]; then
        echo -e "${red}注意： CentOS 7 无法使用hysteria1/2协议！${plain}\n"
    fi
elif [[ x"${release}" == x"ubuntu" ]]; then
    if [[ ${os_version} -lt 16 ]]; then
        echo -e "${red}请使用 Ubuntu 16 或更高版本的系统！${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"debian" ]]; then
    if [[ ${os_version} -lt 8 ]]; then
        echo -e "${red}请使用 Debian 8 或更高版本的系统！${plain}\n" && exit 1
    fi
fi

install_base() {
    # 优化版本：批量检查和安装包，减少系统调用
    need_install_apt() {
        local packages=("$@")
        local missing=()
        
        # 批量检查已安装的包
        local installed_list=$(dpkg-query -W -f='${Package}\n' 2>/dev/null | sort)
        
        for p in "${packages[@]}"; do
            if ! echo "$installed_list" | grep -q "^${p}$"; then
                missing+=("$p")
            fi
        done
        
        if [[ ${#missing[@]} -gt 0 ]]; then
            echo "安装缺失的包: ${missing[*]}"
            apt-get update -y >/dev/null 2>&1
            DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}" >/dev/null 2>&1
        fi
    }

    need_install_yum() {
        local packages=("$@")
        local missing=()
        
        # 批量检查已安装的包
        local installed_list=$(rpm -qa --qf '%{NAME}\n' 2>/dev/null | sort)
        
        for p in "${packages[@]}"; do
            if ! echo "$installed_list" | grep -q "^${p}$"; then
                missing+=("$p")
            fi
        done
        
        if [[ ${#missing[@]} -gt 0 ]]; then
            echo "安装缺失的包: ${missing[*]}"
            yum install -y "${missing[@]}" >/dev/null 2>&1
        fi
    }

    need_install_apk() {
        local packages=("$@")
        local missing=()
        
        # 批量检查已安装的包
        local installed_list=$(apk info 2>/dev/null | sort)
        
        for p in "${packages[@]}"; do
            if ! echo "$installed_list" | grep -q "^${p}$"; then
                missing+=("$p")
            fi
        done
        
        if [[ ${#missing[@]} -gt 0 ]]; then
            echo "安装缺失的包: ${missing[*]}"
            apk add --no-cache "${missing[@]}" >/dev/null 2>&1
        fi
    }

    # 一次性安装所有必需的包
    if [[ x"${release}" == x"centos" ]]; then
        # 检查并安装 epel-release
        if ! rpm -q epel-release >/dev/null 2>&1; then
            echo "安装 EPEL 源..."
            yum install -y epel-release >/dev/null 2>&1
        fi
        need_install_yum wget curl unzip tar cronie socat ca-certificates pv iptables
        update-ca-trust force-enable >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"alpine" ]]; then
        need_install_apk wget curl unzip tar socat ca-certificates pv iptables
        update-ca-certificates >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"debian" ]]; then
        need_install_apt wget curl unzip tar cron socat ca-certificates pv iptables
        update-ca-certificates >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"ubuntu" ]]; then
        need_install_apt wget curl unzip tar cron socat ca-certificates pv iptables
        update-ca-certificates >/dev/null 2>&1 || true
    elif [[ x"${release}" == x"arch" ]]; then
        echo "更新包数据库..."
        pacman -Sy --noconfirm >/dev/null 2>&1
        # --needed 会跳过已安装的包，非常高效
        echo "安装必需的包..."
        pacman -S --noconfirm --needed wget curl unzip tar cronie socat ca-certificates pv iptables >/dev/null 2>&1
    fi
}

# 0: running, 1: not running, 2: not installed
check_status() {
    if [[ ! -f /usr/local/v2node/v2node ]]; then
        return 2
    fi
    if [[ x"${release}" == x"alpine" ]]; then
        temp=$(service v2node status | awk '{print $3}')
        if [[ x"${temp}" == x"started" ]]; then
            return 0
        else
            return 1
        fi
    else
        temp=$(systemctl status v2node | grep Active | awk '{print $3}' | cut -d "(" -f2 | cut -d ")" -f1)
        if [[ x"${temp}" == x"running" ]]; then
            return 0
        else
            return 1
        fi
    fi
}

generate_v2node_config() {
        local api_host="$1"
        local node_id="$2"
        local api_key="$3"

        mkdir -p /etc/v2node >/dev/null 2>&1
        backup_existing_configs
        cat > /etc/v2node/config.yml <<EOF
panel:
  url: "${api_host}"
  token: "${api_key}"
  node_id: ${node_id}
  timeout: 15

kernel:
  config_dir: "/etc/v2node"
  log_level: "warning"
  dns_servers:
    - "1.1.1.1"
    - "8.8.8.8"

log:
  level: "warning"
  output: ""
  access: "none"

runtime:
  gomemlimit: ""
  gogc: 0
  auto_hy2_port_forward: true

health_port: 0
pprof_port: 0
EOF
        echo -e "${green}V2node 配置文件生成完成,正在重新启动服务${plain}"
        if [[ x"${release}" == x"alpine" ]]; then
            service v2node restart
        else
            systemctl restart v2node
        fi
        sleep 2
        check_status
        echo -e ""
        if [[ $? == 0 ]]; then
            echo -e "${green}v2node 重启成功${plain}"
        else
            echo -e "${red}v2node 可能启动失败，请使用 v2node log 查看日志信息${plain}"
        fi
}

yaml_escape() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    printf '%s' "$value"
}

trim_value() {
    local value="$1"
    value="${value#"${value%%[![:space:]]*}"}"
    value="${value%"${value##*[![:space:]]}"}"
    printf '%s' "$value"
}

yaml_unquote() {
    local value
    value=$(trim_value "$1")
    if [[ "$value" == \"*\" && "$value" == *\" ]]; then
        value="${value#\"}"
        value="${value%\"}"
        value="${value//\\\"/\"}"
        value="${value//\\\\/\\}"
    fi
    printf '%s' "$value"
}

normalize_machine_url() {
    local value
    value=$(trim_value "$1")
    while [[ "$value" == */ ]]; do
        value="${value%/}"
    done
    printf '%s' "$value"
}

restart_v2node_service() {
    if [[ x"${release}" == x"alpine" ]]; then
        service v2node restart
    else
        systemctl restart v2node
    fi
    sleep 2
    check_status
    echo -e ""
    if [[ $? == 0 ]]; then
        echo -e "${green}v2node 重启成功${plain}"
    else
        echo -e "${red}v2node 可能启动失败，请使用 v2node log 查看日志信息${plain}"
    fi
}

start_v2node_service() {
    if [[ x"${release}" == x"alpine" ]]; then
        service v2node start
    else
        systemctl start v2node
    fi
    sleep 2
    check_status
    echo -e ""
    if [[ $? == 0 ]]; then
        echo -e "${green}v2node 启动成功${plain}"
    else
        echo -e "${red}v2node 可能启动失败，请使用 v2node log 查看日志信息${plain}"
    fi
}

stop_v2node_service() {
    if [[ x"${release}" == x"alpine" ]]; then
        service v2node stop >/dev/null 2>&1 || true
    else
        systemctl stop v2node >/dev/null 2>&1 || true
    fi
}

extract_machine_profiles() {
    local config_file="$1"
    local line value in_profiles=false in_profile=false
    local name="" url="" token="" machine_id="" timeout="" config_dir=""

    flush_profile() {
        if [[ -n "$url" && -n "$machine_id" ]]; then
            [[ -z "$timeout" ]] && timeout=15
            printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$url" "$token" "$machine_id" "$timeout" "$config_dir"
        fi
        in_profile=false
        name=""
        url=""
        token=""
        machine_id=""
        timeout=""
        config_dir=""
    }

    [[ -f "$config_file" ]] || return 0

    while IFS= read -r line || [[ -n "$line" ]]; do
        if [[ "$line" =~ ^[[:space:]]*profiles:[[:space:]]*$ ]]; then
            in_profiles=true
            continue
        fi

        if [[ "$in_profiles" != true ]]; then
            continue
        fi

        if [[ "$line" =~ ^[[:alnum:]_]+: ]]; then
            flush_profile
            in_profiles=false
            continue
        fi

        if [[ "$line" =~ ^[[:space:]]*-[[:space:]]*name:[[:space:]]*(.*)$ ]]; then
            flush_profile
            in_profile=true
            name=$(yaml_unquote "${BASH_REMATCH[1]}")
            continue
        fi
        if [[ "$line" =~ ^[[:space:]]*-[[:space:]]*url:[[:space:]]*(.*)$ ]]; then
            flush_profile
            in_profile=true
            url=$(normalize_machine_url "$(yaml_unquote "${BASH_REMATCH[1]}")")
            continue
        fi
        if [[ "$in_profile" == true && "$line" =~ ^[[:space:]]*url:[[:space:]]*(.*)$ ]]; then
            url=$(normalize_machine_url "$(yaml_unquote "${BASH_REMATCH[1]}")")
            continue
        fi
        if [[ "$in_profile" == true && "$line" =~ ^[[:space:]]*token:[[:space:]]*(.*)$ ]]; then
            token=$(yaml_unquote "${BASH_REMATCH[1]}")
            continue
        fi
        if [[ "$in_profile" == true && "$line" =~ ^[[:space:]]*machine_id:[[:space:]]*([0-9]+) ]]; then
            machine_id="${BASH_REMATCH[1]}"
            continue
        fi
        if [[ "$in_profile" == true && "$line" =~ ^[[:space:]]*timeout:[[:space:]]*([0-9]+) ]]; then
            timeout="${BASH_REMATCH[1]}"
            continue
        fi
        if [[ "$in_profile" == true && "$line" =~ ^[[:space:]]*config_dir:[[:space:]]*(.*)$ ]]; then
            config_dir=$(yaml_unquote "${BASH_REMATCH[1]}")
            continue
        fi
    done < "$config_file"

    flush_profile
}

write_machine_config_from_profiles() {
    local profiles_file="$1"
    local name url token machine_id timeout config_dir

    cat <<EOF
machine:
  enabled: true
  continue_on_error: true
  profiles:
EOF

    while IFS=$'\t' read -r name url token machine_id timeout config_dir; do
        [[ -z "$url" || -z "$machine_id" ]] && continue
        [[ -z "$name" ]] && name="machine-${machine_id}"
        [[ -z "$timeout" ]] && timeout=15
        cat <<EOF
    - name: "$(yaml_escape "$name")"
      url: "$(yaml_escape "$url")"
      token: "$(yaml_escape "$token")"
      machine_id: ${machine_id}
      timeout: ${timeout}
EOF
        if [[ -n "$config_dir" ]]; then
            cat <<EOF
      config_dir: "$(yaml_escape "$config_dir")"
EOF
        fi
    done < "$profiles_file"

    cat <<EOF

kernel:
  config_dir: "/etc/v2node"
  log_level: "warning"
  dns_servers:
    - "1.1.1.1"
    - "8.8.8.8"

log:
  level: "warning"
  output: ""
  access: "none"

runtime:
  gomemlimit: ""
  gogc: 0
  auto_hy2_port_forward: true

health_port: 0
pprof_port: 0
EOF
}

generate_v2node_machine_config() {
    local machine_url="$1"
    local machine_id="$2"
    local machine_token="$3"
    local machine_name="$4"
    local replace_machine_id="${5:-false}"
    local existing_profiles merged_profiles new_config profile_count

    machine_url=$(normalize_machine_url "$machine_url")
    if [[ -z "$machine_name" ]]; then
        machine_name="machine-${machine_id}"
    fi

    mkdir -p "$V2NODE_CONFIG_DIR" >/dev/null 2>&1
    existing_profiles=$(mktemp)
    merged_profiles=$(mktemp)
    new_config=$(mktemp)

    extract_machine_profiles "$V2NODE_CONFIG_FILE" > "$existing_profiles"
    awk -F '\t' \
        -v name="$machine_name" \
        -v url="$machine_url" \
        -v token="$machine_token" \
        -v machine_id="$machine_id" \
        -v replace_machine_id="$replace_machine_id" \
        'BEGIN { updated = 0 }
         {
             if (($2 == url && $4 == machine_id) || ($3 == token && $4 == machine_id) || (replace_machine_id == "true" && $4 == machine_id)) {
                 if (!updated) {
                     print name "\t" url "\t" token "\t" machine_id "\t15\t"
                     updated = 1
                 }
                 next
             }
             print $0
         }
         END {
             if (!updated) {
                 print name "\t" url "\t" token "\t" machine_id "\t15\t"
             }
         }' "$existing_profiles" > "$merged_profiles"

    write_machine_config_from_profiles "$merged_profiles" > "$new_config"
    if [[ -f "$V2NODE_CONFIG_FILE" ]] && cmp -s "$new_config" "$V2NODE_CONFIG_FILE"; then
        rm -f "$existing_profiles" "$merged_profiles" "$new_config"
        echo -e "${green}V2node machine 配置未变化，保持现有 profiles${plain}"
        restart_v2node_service
        return
    fi

    backup_existing_configs
    mv -f "$new_config" "$V2NODE_CONFIG_FILE"
    rm -f "$existing_profiles" "$merged_profiles"
    profile_count=$(extract_machine_profiles "$V2NODE_CONFIG_FILE" | wc -l | tr -d ' ')
    echo -e "${green}V2node machine 配置已合并，当前 profiles 数量: ${profile_count}${plain}"
    echo -e "${green}同一台 VPS 多个 Xboardpro 仍由一个 v2node 进程承载${plain}"
    restart_v2node_service
}

backup_existing_configs() {
    local now
    now=$(date +%Y%m%d%H%M%S)
    for config_file in /etc/v2node/config.json /etc/v2node/config.yml /etc/v2node/config.yaml; do
        if [[ -f "$config_file" ]]; then
            mv -f "$config_file" "${config_file}.bak.${now}"
            echo -e "${yellow}已备份旧配置: ${config_file}.bak.${now}${plain}"
        fi
    done
}

write_default_v2node_config() {
    mkdir -p "$V2NODE_CONFIG_DIR" >/dev/null 2>&1
    cat > "$V2NODE_CONFIG_FILE" <<'EOF'
panel:
  url: "https://example.com/"
  token: "your-node-token"
  node_id: 1
  timeout: 15

kernel:
  config_dir: "/etc/v2node"
  log_level: "warning"
  dns_servers:
    - "1.1.1.1"
    - "8.8.8.8"

log:
  level: "warning"
  output: ""
  access: "none"

runtime:
  gomemlimit: ""
  gogc: 0

health_port: 0
pprof_port: 0
EOF
}

resolve_v2node_version() {
    local version_param="$1"

    if [[ -z "$version_param" ]] ; then
        last_version=$(curl -Ls "https://api.github.com/repos/keli-123456/kelinode/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        if [[ ! -n "$last_version" ]]; then
            echo -e "${red}检测 v2node 版本失败，可能是超出 Github API 限制，请稍后再试，或手动指定 v2node 版本安装${plain}"
            exit 1
        fi
    else
        last_version=$version_param
    fi

    url="https://github.com/keli-123456/kelinode/releases/download/${last_version}/v2node-linux-${arch}.zip"
}

installed_v2node_version() {
    local installed=""

    if [[ -f "$V2NODE_VERSION_FILE" ]]; then
        read -r installed < "$V2NODE_VERSION_FILE" || true
    fi
    if [[ -z "$installed" && -x "${V2NODE_INSTALL_DIR}/v2node" ]]; then
        installed=$("${V2NODE_INSTALL_DIR}/v2node" version 2>/dev/null | awk '{print $2}' | head -n 1)
    fi

    printf '%s' "$installed"
}

is_v2node_installed_version() {
    local expected="$1"
    local installed

    [[ -x "${V2NODE_INSTALL_DIR}/v2node" ]] || return 1
    [[ -f "${V2NODE_INSTALL_DIR}/geoip.dat" ]] || return 1
    [[ -f "${V2NODE_INSTALL_DIR}/geosite.dat" ]] || return 1

    installed=$(installed_v2node_version)
    [[ "$installed" == "$expected" ]]
}

install_v2node_service() {
    if [[ x"${release}" == x"alpine" ]]; then
        rm /etc/init.d/v2node -f
        cat <<EOF > /etc/init.d/v2node
#!/sbin/openrc-run

name="v2node"
description="v2node"

command="${V2NODE_INSTALL_DIR}/v2node"
command_args="server"
command_user="root"

pidfile="/run/v2node.pid"
command_background="yes"

depend() {
        need net
}
EOF
        chmod +x /etc/init.d/v2node
        rc-update add v2node default
    else
        rm /etc/systemd/system/v2node.service -f
        cat <<EOF > /etc/systemd/system/v2node.service
[Unit]
Description=v2node Service
After=network.target nss-lookup.target
Wants=network.target

[Service]
User=root
Group=root
Type=simple
LimitAS=infinity
LimitRSS=infinity
LimitCORE=infinity
LimitNOFILE=999999
WorkingDirectory=${V2NODE_INSTALL_DIR}/
ExecStart=${V2NODE_INSTALL_DIR}/v2node server
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl enable v2node
    fi
}

download_and_install_v2node_binary() {
    stop_v2node_service
    rm -rf "$V2NODE_INSTALL_DIR"
    mkdir -p "$V2NODE_INSTALL_DIR"
    cd "$V2NODE_INSTALL_DIR" || exit 1

    echo -e "${green}检测到需要安装 v2node ${last_version}，开始下载...${plain}"
    if ! curl -fL "$url" -o "${V2NODE_INSTALL_DIR}/v2node-linux.zip"; then
        echo -e "${red}下载 v2node ${last_version} 失败，请确认版本存在且服务器可以访问 Github Release${plain}"
        exit 1
    fi

    unzip -o v2node-linux.zip
    rm v2node-linux.zip -f
    chmod +x v2node
    mkdir -p "$V2NODE_CONFIG_DIR"
    cp geoip.dat "$V2NODE_CONFIG_DIR/"
    cp geosite.dat "$V2NODE_CONFIG_DIR/"
    printf '%s\n' "$last_version" > "$V2NODE_VERSION_FILE"
    echo -e "${green}v2node ${last_version}${plain} 安装完成"
}

install_v2node() {
    local version_param="$1"
    resolve_v2node_version "$version_param"

    if is_v2node_installed_version "$last_version"; then
        echo -e "${green}已安装 v2node ${last_version}，跳过二进制下载${plain}"
    else
        download_and_install_v2node_binary
    fi
    install_v2node_service
    echo -e "${green}v2node ${last_version}${plain} 已设置开机自启"

    if has_machine_args; then
        generate_v2node_machine_config "$MACHINE_URL_ARG" "$MACHINE_ID_ARG" "$MACHINE_TOKEN_ARG" "$MACHINE_NAME_ARG" "$MACHINE_REPLACE_ID_ARG"
        echo -e "${green}已根据 machine 参数生成 /etc/v2node/config.yml${plain}"
        first_install=false
    elif [[ ! -f /etc/v2node/config.json && ! -f "$V2NODE_CONFIG_FILE" && ! -f /etc/v2node/config.yaml ]]; then
        # 如果通过 CLI 传入了完整参数，则直接生成配置并跳过交互
        if [[ -n "$API_HOST_ARG" && -n "$NODE_ID_ARG" && -n "$API_KEY_ARG" ]]; then
            generate_v2node_config "$API_HOST_ARG" "$NODE_ID_ARG" "$API_KEY_ARG"
            echo -e "${green}已根据参数生成 /etc/v2node/config.yml${plain}"
            first_install=false
        else
            write_default_v2node_config
            first_install=true
        fi
    else
        start_v2node_service
        first_install=false
    fi


    curl -o /usr/bin/v2node -Ls https://raw.githubusercontent.com/keli-123456/kelinode/main/script/v2node.sh
    chmod +x /usr/bin/v2node

    cd $cur_dir
    rm -f install.sh
    echo "------------------------------------------"
    echo -e "管理脚本使用方法: "
    echo "------------------------------------------"
    echo "v2node              - 显示管理菜单 (功能更多)"
    echo "v2node start        - 启动 v2node"
    echo "v2node stop         - 停止 v2node"
    echo "v2node restart      - 重启 v2node"
    echo "v2node status       - 查看 v2node 状态"
    echo "v2node enable       - 设置 v2node 开机自启"
    echo "v2node disable      - 取消 v2node 开机自启"
    echo "v2node log          - 查看 v2node 日志"
    echo "v2node generate     - 生成 v2node 配置文件"
    echo "v2node update       - 更新 v2node"
    echo "v2node update x.x.x - 更新 v2node 指定版本"
    echo "v2node install      - 安装 v2node"
    echo "v2node uninstall    - 卸载 v2node"
    echo "v2node version      - 查看 v2node 版本"
    echo "------------------------------------------"
    curl -fsS --max-time 10 "https://api.v-50.me/counter" || true

    if [[ $first_install == true ]]; then
        read -rp "检测到你为第一次安装 v2node，是否自动生成 /etc/v2node/config.yml？(y/n): " if_generate
        if [[ "$if_generate" =~ ^[Yy]$ ]]; then
            # 交互式收集参数，提供示例默认值
            read -rp "面板API地址[格式: https://example.com/]: " api_host
            api_host=${api_host:-https://example.com/}
            read -rp "节点ID: " node_id
            node_id=${node_id:-1}
            read -rp "节点通讯密钥: " api_key

            # 生成配置文件（覆盖可能从包中复制的模板）
            generate_v2node_config "$api_host" "$node_id" "$api_key"
        else
            echo "${green}已跳过自动生成配置。如需后续生成，可执行: v2node generate${plain}"
        fi
    fi
}

parse_args "$@"
validate_args
echo -e "${green}开始安装${plain}"
acquire_install_lock
install_base
install_v2node "$VERSION_ARG"
