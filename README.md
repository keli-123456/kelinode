# v2node
A v2board backend base on moddified xray-core.
一个基于修改版xray内核的V2board节点服务端。

**注意： 本项目需要搭配[修改版V2board](https://github.com/wyx2685/v2board)**

当前 `main` 已切到 Xray Core 26.x 兼容基线，依赖的是自有 `keli-core` 26.x 兼容 fork。

## 软件安装

### 一键安装

仅适用于非 Docker 裸机安装：

```
wget -N https://raw.githubusercontent.com/keli-123456/kelinode/main/script/install.sh && bash install.sh
```

安装脚本和 `v2node generate` 现在默认生成 `/etc/v2node/config.yml`，并在覆盖旧配置前自动备份已有的 `config.json` / `config.yml` / `config.yaml`。

### Docker 部署

推荐直接使用镜像：

```bash
docker run -d --name v2node \
  --restart unless-stopped \
  --network host \
  -v /opt/v2node:/etc/v2node \
  -e V2NODE_API_HOST="https://panel.example.com" \
  -e V2NODE_NODE_ID="1" \
  -e V2NODE_API_KEY="your-node-token" \
  -e V2NODE_NODE_CONFIG_DIR="/etc/v2node" \
  -e V2NODE_HEALTH_PORT="65530" \
  -e V2NODE_GOMEMLIMIT="512MiB" \
  -e V2NODE_GOGC="100" \
  ghcr.io/keli-123456/v2node:main
```

说明：

- 未显式指定 `V2NODE_CONFIG_PATH` 时，容器入口默认使用 `/etc/v2node/config.yml`
- 如果 `/etc/v2node/config.yml` 不存在，会根据环境变量自动生成 YAML v2 配置
- 如果目录里只有旧 `/etc/v2node/config.json`，仍会自动兼容加载
- 建议挂载 `/etc/v2node`，避免重建容器后丢失生成的配置、证书和本地状态文件

详细部署说明见：

- [Docker Deployment Guide](./docs/docker-deployment.md)
- [Xray 26.3.27 Upgrade Release Checklist](./docs/release-checklist-xray-26.3.27.md)

## 构建
``` bash
GOEXPERIMENT=jsonv2 go build -v -o build_assets/v2node -trimpath -ldflags "-X 'github.com/keli-123456/kelinode/cmd.version=$version' -s -w -buildid="
```

构建环境建议使用 Go 1.26+。

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
