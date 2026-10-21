# 32 · 出站 HTTP 策略（infra/outbound）

> 一次"运维配的端点被 SSRF 校验误伤"的排查，最后变成把整个出站策略收进一个包。

## 1. 问题：策略写在每个组件里

v1.75.0 为了堵 SSRF，给几条出站路径各自加了一道网守。每条都"默认拒绝内网、回环、云元数据地址"——方向没错，但落地方式是按组件各写一套：

| 组件 | 当时的做法 | 结果 |
| --- | --- | --- |
| websearch | 搜索与抓取**共用**一条受守卫的 Transport | 给搜索端点开口子，抓取路径跟着变松；不给就整体不可用 |
| sauce | 受信媒体名单挂在自己的 Transport 上 | 同一 Transport 的其它下载一并变松 |
| AI 附件 | 独立客户端 + 独立名单 | 名单只覆盖运维手写的主机，平台自建媒体接口默认全部被丢 |
| aimage | 自己实现"同源即可信" | 与 AI 附件的语义又不一致 |
| MCP | **每次拨号前**重做一遍 IP 校验 | 自建服务器被打成"连不上"，且完全不读代理环境变量 |

同类问题因此反复出现，而失败表现一律是"功能坏了"：拦截大多只留一行 Debug 日志，运维看不出这是策略的决定。

根本原因不是某个校验写错了，而是**目标性质不同的请求被塞进了同一套策略**：

- 用户/模型给出的 URL：不可信，防护必须完整；
- 运维写在配置里的端点（自建 SearXNG、MCP 服务器、图片后端）：是运维的信任决定，套公网校验挡不住攻击者（攻击者改不了这段配置），只会误伤；
- 平台/框架自己产生的地址（适配器托管的媒体源）：只应信任被声明的那几个源。

## 2. 设计：显式选类 + 构造期固化

新增 `infra/outbound`。调用方必须显式给目标选一个类，零值落到最严：

| 类 | 适用目标 | 准入 | 重定向 |
| --- | --- | --- | --- |
| `Untrusted` | 用户/模型给出的 URL | 仅 `https` + 公网；禁止 URL 用户信息 | 逐跳按同一策略校验 |
| `TrustedOrigin` | 框架/平台产生的地址 | 命中所声明 `Origins` 可走内网/明文；跨源仍严格 | 逐跳校验，受信判定重新做过 |
| `OperatorEndpoint` | 运维配置的端点 | 同 `TrustedOrigin`，但 `Origins` 必须非空 | 同上；可选 `SameOriginOnly` |

要点：

- **零值是 `Untrusted`**。新增路径忘了选类时不会变松。
- **`OperatorEndpoint` 拒绝空来源**。写了"这是受信端点"却没告诉策略信谁，构造期直接报错，而不是运行期全被拦。
- **策略在构造期固化**。`Class`/`Origins` 只读，`Client` 可并发使用，不存在"请求期改共享字段"的竞态，也不存在"某条路径继承了别人的口子"。
- **受信来源只来自运维配置或适配器实现**。用户输入拼进 `Origins` 等于对该目标开放直连。

`Origins` 的写法复用 `netguard.HostAllowlist` 的语义（`host` / `host:port` / 完整 URL），让 URL 准入、拨号名单、重定向判定共用一套解析，不引入第二套规则。

## 3. 执行层兜底

准入只判 URL 文本，真正的防护在拨号：

1. 解析主机名；
2. 要求**全部**解析结果都是公网（不只是第一个）；
3. **直连已校验的 IP**，而不是按域名再解析一次——把 DNS 重绑定的窗口关掉，而不是缩小；
4. 代理地址（显式 `Proxy` 或环境变量）单独识别后直接拨号：代理常在本机回环，参与公网校验会让"需要代理才能出网"的部署全线失败（sauce / websearch / AI 附件都踩过）；
5. 受信来源命中后直接拨号，不再校验（这就是"运维的信任决定"的落地处）。

重定向由 `CheckRedirect` 逐跳处理：跳数上限、目标重新过一遍准入、`SameOriginOnly` 时要求停留在 `Origins[0]` 的同源地址。

## 4. 可观测性

- 指标 `outbound_blocked_total{component,reason}`：每次拦截都计数，`reason` 是稳定枚举（`malformed` / `userinfo` / `scheme` / `plain-http` / `private-address` / `too-many-redirects` / `cross-origin-redirect`）。
- 日志：每个 `(component, reason)` 每分钟最多一条 Warn，避免刷屏；URL 经 `sanitize()` 去掉 userinfo、查询串与 fragment（查询串常含 token）。
- 构造期以 Info 记录一次 `component` / `class` / `origins` 数量：运维能直接看到某条路径被分到了哪一类，不必等到功能不可用。
- `BlockError.Error()` 只返回中文原因（面向用户，出现在命令回复/工具结果里）；组件名只进指标与日志。

用 `outbound.Blocked(err)` 区分"被策略拦下"和"网络故障"，据此给出可读提示。

## 5. 平台自声明媒体源

附件地址由平台适配器产生。协议层常常就是这么设计的：OneBot 实现（NapCat / LLOneBot）把收到的图片、语音、文件挂在自己的文件服务上，地址往往是 `http://127.0.0.1:3000`，与 WebSocket API 端口（如 `ws://127.0.0.1:6700`）**不是同一个**。

与其让运维为每个插件手抄主机名，不如让最清楚这件事的适配器声明：

```go
// platform.MediaOriginProvider
MediaOrigins() []string
```

`Registry.MediaOrigins()` 汇总所有适配器（去重保序），插件在 Setup 期读取一次：

- OneBot：优先 `media_base_url`；留空时按 API 端点同源推导（`ws`→`http`、`wss`→`https`）。ReverseWS 没有对端 URL，因此只认显式配置。
- 插件侧把「适配器声明 + `allow_private_media_hosts`」合并为 `TrustedOrigin` 的来源。`allow_private_media_hosts` 相应降级为**补充名单**（内网图床等适配器不知道的来源）。

读取发生在 Setup 期而非每个事件：策略对象是静态句柄，不做 per-event 动态客户端。

## 6. 迁移落点

| 路径 | 类 | 来源 |
| --- | --- | --- |
| websearch 抓取（`/fetch`、`fetch_url`） | `Untrusted` | — |
| websearch 搜索（`/search`、`web_search`） | `OperatorEndpoint` | 搜索端点 |
| AI 附件下载 | `TrustedOrigin` | 平台声明 + `allow_private_media_hosts` |
| AI `send_message.image_url` | `Untrusted`（`outbound.Allows`，只判定不下载） | — |
| sauce 图片下载 | `TrustedOrigin` | 平台声明 + `allow_private_media_hosts` |
| aimage 生成端点 / 结果下载 | `OperatorEndpoint` / `TrustedOrigin` | `base_url` |
| MCP HTTP 传输 | `OperatorEndpoint` + `SameOriginOnly` | 服务器 URL |

MCP 是"保留严格度、去掉误伤"的样板：`allow_insecure_http` / `allow_private_networks` 语义不变，强制 TLS 1.2+ 与 `ca_cert_file` 不变，重定向强制同源（跨源会把自定义认证头转发给另一台服务器）不变；变的只有校验时机——从**每次拨号**改为**启动期解析一次**。端点由运维写死，攻击者影响不了配置，逐请求重校验没有安全收益。

## 7. 守卫

架构守卫（`infra/outbound/architecture_test.go`）扫描全仓 Go 源码，禁止 `infra/outbound` 之外的业务代码引用 `infra/netguard`。新增出站路径请用 `outbound.New` 选一个类，而不是另起一套校验——否则就会退回到"每个组件一套策略"，误伤与漏防会再次并存。
