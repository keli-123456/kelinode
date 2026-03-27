# v2node
A v2board backend base on moddified xray-core.
一个基于修改版xray内核的V2board节点服务端。

**注意： 本项目需要搭配[修改版V2board](https://github.com/wyx2685/v2board)**

## 软件安装

### 一键安装

```
wget -N https://raw.githubusercontent.com/keli-123456/kelinode/main/script/install.sh && bash install.sh
```

安装脚本和 `v2node generate` 现在默认生成 `/etc/v2node/config.yml`，并在覆盖旧配置前自动备份已有的 `config.json` / `config.yml` / `config.yaml`。

## 构建
``` bash
GOEXPERIMENT=jsonv2 go build -v -o build_assets/v2node -trimpath -ldflags "-X 'github.com/keli-123456/kelinode/cmd.version=$version' -s -w -buildid="
```

## 运行时与健康检查

`v2node` 现已支持以下运维参数：

- `HealthPort`: 本地健康检查端口，`0` 表示关闭
- `Runtime.GoMemLimit`: Go 运行时软内存上限，例如 `256MiB`
- `Runtime.GOGC`: Go GC 目标百分比，`0` 表示保持默认值

健康检查端点：

- `/livez`
- `/readyz`
- `/health`
- `/healthz`

Docker 环境变量：

- `V2NODE_HEALTH_PORT`
- `V2NODE_GOMEMLIMIT`
- `V2NODE_GOGC`
- `V2NODE_NODE_CONFIG_DIR`
- `V2NODE_CONFIG_PATH`

## 配置兼容

`v2node` 现在同时支持：

- 旧版 `config.json`
- 新版 `config.yml` / `config.yaml`

默认会优先读取你显式传入的 `--config`，否则在 `/etc/v2node/` 下自动回退：

1. `config.json`
2. `config.yml`
3. `config.yaml`

脚本和 Docker 入口在未指定 `V2NODE_CONFIG_PATH` 时，默认会生成或使用 `/etc/v2node/config.yml`；如果该文件不存在，仍会自动兼容回退到旧 `config.json`。

新版 `config.yml v2` 可用字段见 [config.yml.example](./config.yml.example)。

## Stars 增长记录

[![Stargazers over time](https://starchart.cc/keli-123456/kelinode.svg?variant=adaptive)](https://starchart.cc/keli-123456/kelinode)
