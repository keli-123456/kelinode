# Kelinode AI Rules

这些规则适用于 `kelinode`。根目录 `AGENTS.md` 的全局规则同样适用。

## 节点端职责

`kelinode` 是节点执行端，负责：

- 和 `keliboard` 对接。
- 拉取节点配置。
- 拉取用户或增量用户。
- 启动和维护协议核心。
- 上报流量、在线用户、运行状态。
- 维护 realtime 连接。
- 维护 HY2 UDP 端口转发规则。
- 同时支持 Docker 部署和二进制部署。

## 兼容性底线

任何改动都不能无意破坏：

- Docker 直接对接节点。
- 二进制通过服务器绑定节点。
- 单站点单节点。
- 多站点多节点。
- 旧版面板字段。
- 已运行节点的现有配置。

如果必须改变兼容行为，必须说明迁移方案和回滚方式。

## 面板对接规则

涉及 API 时必须确认：

- `keliboard` 的 route/controller/service。
- 认证字段和 token 来源。
- 机器绑定节点逻辑。
- 节点 ID、code、machine_id、runtime 的含义。
- Docker 节点和二进制节点是否走同一路径。

禁止：

- 手写假节点 ID。
- 猜测 API 字段。
- 忽略 `runtime` 对部署模式的影响。
- 把机器 token 和节点 token 混用。

## realtime 规则

realtime 连接必须明确：

- public URL 如何从 `keliboard` 设置解析。
- fallback 到 `app_url` 时的行为。
- DNS 失败时如何记录。
- websocket 断开是否会重连。
- 多站点时 tag 是否能定位到站点和节点。

日志必须能区分：

- DNS lookup 失败。
- websocket 异常关闭。
- 认证失败。
- 服务端返回 400。
- 用户同步失败。
- 上报失败。

## HY2 端口转发规则

HY2 自动端口转发必须满足：

- 端口段为空时不生成错误规则。
- 端口段非法时给出明确错误。
- 多节点端口段不能互相覆盖。
- 多站点多节点时不能覆盖彼此规则。
- 已存在规则时不能重复添加。
- 节点删除、解绑、端口段修改时要能清理旧规则。
- IPv4 和 IPv6 规则状态要一致。

规则状态命令必须可用，例如：

```bash
sudo /usr/local/v2node/v2node rules status -c /etc/v2node/config.yml
sudo iptables -t nat -S PREROUTING | grep V2NODE-HY2
sudo ip6tables -t nat -S PREROUTING | grep V2NODE-HY2
sudo iptables -t nat -nvL PREROUTING --line-numbers | grep V2NODE-HY2
sudo ip6tables -t nat -nvL PREROUTING --line-numbers | grep V2NODE-HY2
sudo ss -lunp
```

## 配置变更规则

修改节点配置时必须说明：

- 是否热重载。
- 是否会短暂断线。
- 是否会重启核心。
- 是否会重建 iptables/ip6tables 规则。
- 是否影响正在连接的用户。

如果当前实现不能热重载，不要假装支持。

## 日志规则

日志必须帮助定位问题。

关键日志需要包含：

- panel base URL。
- 节点类型和节点 ID/code。
- machine_id 或绑定关系，避免泄露 token。
- runtime。
- realtime URL。
- 端口段和本地监听端口。
- 规则添加、已存在、删除、失败原因。

禁止在日志中输出完整 token、密钥、用户密码。

## 验证要求

改 Go 后运行：

```bash
go test ./...
```

并尽量构建目标二进制。

涉及 systemd 或二进制运行时，给出：

```bash
journalctl -u v2node -n 100 --no-pager
journalctl -u v2node -n 100 --no-pager | grep -Ei "Realtime|Invalid|lookup|report|user_delta"
```

涉及 Docker 兼容性，必须说明 Docker 部署是否需要额外规则或命令。

## 修改边界

禁止为了一个节点问题大范围重写：

- 配置模型。
- API 客户端。
- runtime 管理。
- 规则管理。

除非用户明确要求重构，否则优先做小而可验证的修复。
