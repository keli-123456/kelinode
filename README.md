# v2node
A v2board backend base on moddified xray-core.
一个基于修改版xray内核的V2board节点服务端。

**注意： 本项目需要搭配[修改版V2board](https://github.com/wyx2685/v2board)**

## 软件安装

### 一键安装

```
wget -N https://raw.githubusercontent.com/keli-123456/kelinode/main/script/install.sh && bash install.sh
```

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

## Stars 增长记录

[![Stargazers over time](https://starchart.cc/keli-123456/kelinode.svg?variant=adaptive)](https://starchart.cc/keli-123456/kelinode)
