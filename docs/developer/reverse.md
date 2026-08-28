---
title: "反向端口转发"
---

Trojan-Go-Next 的反向端口转发（reverse）允许一个已通过认证的客户端请求服务端绑定端口，并把该端口的入站连接逐条回传给客户端，由客户端转发到自己可达的目标（如内网 SSH）。效果等同于 frp 的 TCP 端口映射，但全部流量都封装在既有的 TLS+trojan 会话内，不新增任何对外监听。

## 位置与命令字

客户端在普通 trojan 连接上发送命令字 `4`（`Reverse`），地址域填写哨兵域名 `REVERSE_CONN`（端口为欲绑定的端口）。服务端 trojan 层鉴权通过后，把该连接分发给 reverse 层，协议栈自顶向下为：

| 协议 | 角色 | 备注 |
| ---- | ---- | ---- |
| 控制帧 / 业务流 | 客户端 Accept / 服务端 Open | 见下文 |
| smux | 客户端为 smux Server，服务端为 smux Client | 与 mux 相反：流由服务端打开 |
| Trojan | 命令字 4，鉴权 | |
| 底层协议 | TLS / WebSocket 等 | |

客户端扮演 smux 的 **Server** 角色：每有一个访客连上服务端端口，服务端就 `OpenStream` 一条新流，客户端 `AcceptStream` 得到对应的连接。

## 控制协议

会话建立后，服务端打开的第一条流为控制流。帧格式为手写二进制：

```
+------+--------+--------+---------+
| ver  | type   | length | payload |
| 1B   | 1B     | 2B BE  | 变长    |
+------+--------+--------+---------+
```

ver 固定 `0x01`。帧类型：

| type | 名称 | payload | 说明 |
| ---- | ---- | ------- | ---- |
| 0x01 | BindReq | port u16 + flags u8 + reserved u8 | 客户端请求绑定端口 |
| 0x02 | BindResp | code u8 + port u16 + msg（UTF-8 变长） | 服务端裁决结果 |
| 0x03 | Ping | nonce u64 | 客户端保活 |
| 0x04 | Pong | nonce u64 | 服务端回显 nonce |
| 0x05 | UnbindReq | port u16 | 客户端主动解绑 |

BindResp 的 code：

| code | 含义 |
| ---- | ---- |
| 0 | 绑定成功 |
| 1 | ACL 拒绝（用户不在 reverse.users、端口不在 allow-ports、端口 <1024） |
| 2 | 端口已被占用（含 net.Listen 失败） |
| 3 | 参数非法 |
| 4 | 超出该用户的 max-listeners |

绑定成功后，服务端 `net.Listen(bind-host:port)`，循环 `Accept`：每个访客连接对应 `session.OpenStream` 的一条流，双向 `io.Copy`。客户端 `DialReverse` 返回的 `ReverseListener` 提供 `Accept`/`Serve(target)` 两种消费方式，会话断开后按指数退避（初始 3s、上限 60s、带抖动）自动重连并重发 BindReq。

## 服务端安全约束

1. **ACL 先于绑定**：SHA224(password) 必须命中 `reverse.users`；端口必须落在该用户的 `allow-ports`（支持 `27061-27099` 段或 `27061` 单口）；小于 1024 的特权端口一律拒绝。全部检查在 `net.Listen` 之前完成。
2. **端口所有权互斥**：全局 `port → session` 注册表，端口被任何身份占用即返回 code 2。
3. **级联回收**：会话断开（控制流读错、smux 超时、TCP 断开）即通过 context 取消关闭该身份的全部 listener 与全部流，不产生孤儿端口。
4. **无独立入口**：reverse 只存在于已通过 TLS+trojan 认证的连接内部，没有可被扫描的独立监听端口。
5. **可审计**：bind/unbind/每条访客连接均落日志，含时间、hash 前缀、端口与访客 IP。
6. **限流**：每个 listener 并发上限默认 128，超限直接关闭新访客连接（不排队）；每条流受空闲超时约束。
7. **不影响正常代理**：不在 `reverse.users` 内的合法 trojan 用户发起 reverse 会被拒（code 1），其普通代理能力不受影响。

## 配置

服务端（示例见 `example/server.yaml`）：

```yaml
reverse:
  enabled: true
  default-bind-host: 127.0.0.1
  idle-timeout: 60
  stream-buffer: 262144
  users:
    - password: your_password
      allow-ports: ['27061-27099']
      bind-host: 127.0.0.1
      max-listeners: 1
```

客户端（嵌入方调用 `reverse.Client.DialReverse`，daemon 模式可用 `forwards` 配置）：

```yaml
reverse:
  enabled: true
  forwards:
    - remote-port: 27061
      target: 127.0.0.1:22
```

`ReverseListener` 的 API：`Accept()`（手动消费）、`Serve(target)`（自动转发到本地目标）、`Addr()`、`Done()`（永久失效信号）、`Close()`（发送 UnbindReq 并拆除会话）。`DialOption` 支持 `WithTarget` 与 `WithRetry(RetryPolicy{Initial, Max})`。

绑定被拒时 `DialReverse` 返回 `*reverse.BindError`，可按 `Code` 区分拒绝原因。
