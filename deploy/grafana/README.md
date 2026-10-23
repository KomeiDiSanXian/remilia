# Grafana 看板（Remilia Bot）

`remilia-bot.json` 是 Grafana "Remilia Bot" 看板的完整定义，由
`scripts/gen-grafana-dashboard.py` 生成。改面板请改生成脚本，再重新生成：

```bash
python scripts/gen-grafana-dashboard.py deploy/grafana/remilia-bot.json
```

这样面板差异在 PR 里可读、可回滚；直接在看板里手改会被下次生成覆盖。

## 导入 / 更新

UI：Dashboards → New → Import → 上传 `remilia-bot.json`（勾选 overwrite，UID 保持
`57ab5c4a-525b-4215-afb9-1155dfdc4d92` 即可原地更新）。

API：

```bash
# 1) 把看板包成 legacy API 的请求体
python3 -c 'import json; d=json.load(open("deploy/grafana/remilia-bot.json")); print(json.dumps({"dashboard": d, "overwrite": True, "message": "import remilia-bot"}))' > /tmp/import-dashboard.json

# 2) 覆盖导入（UID 不变，原地更新）
curl -u admin:<password> -X POST http://<grafana>:3000/api/dashboards/db \
  -H 'Content-Type: application/json' --data-binary @/tmp/import-dashboard.json
```

数据源通过看板变量 `datasource` 选择，`job` 变量默认 `remilia-bot`。

## 面板结构

| 行 | 面板 | 类型 | 说明 |
| --- | --- | --- | --- |
| 概览 | 采集状态 / 运行时长 | stat | `up`、`time()-process_start_time_seconds`，抓取断了先看这里 |
| 概览 | 缓存命中率 1h / 24h | stat | `prompt_cached / prompt`，阈值 50/70/80% |
| 概览 | 事件处理速率 / 未命中 token 1h / RSS / Goroutine / 熔断器 / panic+超时 / 出站 / LLM 调用 | stat | 一眼看健康度 |
| AI · 前缀缓存与成本 | 前缀缓存命中率（按用途） | timeseries | `by (kind)`；`chat` 才是主对话 |
| AI · 前缀缓存与成本 | Token 用量速率（按类型） | timeseries | prompt / prompt_cached / completion / reasoning |
| AI · 前缀缓存与成本 | LLM 调用速率（按用途） | timeseries | `by (kind)` |
| AI · 前缀缓存与成本 | LLM 调用结果（24h） | piechart | ok / error / stopped |
| AI · 前缀缓存与成本 | 单次调用平均输入（按用途） | timeseries | prompt token / 调用数 |
| AI · 前缀缓存与成本 | LLM 延迟分位 / 延迟分布 | timeseries + heatmap | 双峰、长尾用热力图看 |
| AI · 首字延迟与吞吐 | 首字延迟 p50/p95、生成速度 p50/p95 | timeseries | `ai_llm_ttft_seconds`、`ai_llm_tokens_per_second`；区分“慢在等”与“慢在写”，需新构建才有数据 |
| AI · 前缀缓存与成本 | 推理 token 占比 | timeseries | reasoning/completion 接近 1 = 输出预算被思考吃掉 |
| AI · 前缀缓存与成本 | 工具集变更（24h，按原因） | bargauge | init/grow/shrink/decay |
| AI · 前缀缓存与成本 | 工具集规模（中位）/ 工具集大小分布 | stat + heatmap | `ai_toolset_size` 直方图 |
| AI · 前缀缓存与成本 | 目录变更 / 目录代数 | bargauge + stat | add/remove/update 与 generation |
| AI · 前缀缓存与成本 | 任务状态迁移 / 任务创建 / 工具调用 Top10 | bargauge + stat | `ai_task_*`、`ai_tool_calls_total` |
| AI · 回合质量与失败 | 回合结果 / 耗时 / 规模、结束原因、失败分类、工具耗时与超时、审批、记忆/RAG | bargauge + timeseries + pie + stat | 回合是用户感知的单位；`ai_turn_*`、`ai_llm_finish/errors`、`ai_tool_duration/timeouts`、`ai_approval_*` |
| AI · 上下文维护 | 会话数/裁剪、图片剥离、工具选择路径、记忆事实 | stat + bargauge + pie | `ai_session_active`、`ai_context_trimmed_messages_total`、`ai_images_dropped_total`、`ai_tool_select_total` |
| 事件管线 · 丢弃与背压 | 背压丢弃、限流拒绝/上限、ExecPool 回退/队列 | timeseries + stat | 唯一会静默丢事件的链路 |
| 平台与出站健康 | 适配器状态、断连/致命、Token 刷新、Webhook 4xx、出站速率 | stat + timeseries | `remilia_adapter_*`、`remilia_token_refresh_failures_total`、`remilia_webhook_requests_total` |
| 插件加载 | 加载耗时 p95、失败原因 | timeseries + bargauge | config/setup 分阶段；失败按 config/setup/timeout/panic |
| 消息链路延迟 | 消息查询 p95、附件下载 p95 | timeseries | `messagelog_query_duration_seconds`、`messagelog_attachments_download_duration_seconds` |
| 消息记录与附件 | 记录/落盘速率、队列与 spool 积压、最旧积压 | timeseries + stat | spool 积压 = 落盘跟不上 |
| 消息记录与附件 | 丢弃/写失败 1h、SQLite 写延迟 p95、热缓存/淘汰 | stat + timeseries | 开 spool 后队列丢弃应恒 0 |
| 消息记录与附件 | 附件状态分布、下载速率、失败率、磁盘占用、下载队列、GC | pie + timeseries + stat | `messagelog_attachments_*` |
| 事件与平台 | 事件入口 / 内部事件 / 处理速率 / Handler p95 | timeseries | 入口按平台+类型，内部事件按类型，两者分开统计 |
| 事件与平台 | 命令 Top10、去重丢弃、熔断打开次数 | bargauge + timeseries + stat | |
| 事件与平台 | 出站发送速率 / 被策略拦截 / 发送延迟 p95 | timeseries | 只覆盖经 `ctx.Reply*` 的出站 |
| 进程与 Go 运行时 | CPU / 内存 / Goroutine·线程 / GC / FD / 抓取耗时 | timeseries | |
| 插件 · Minecraft（折叠） | 查询速率、结果、p95、缓存命中率、GS4/RCON | 5 个 | 未使用时为空 |
| AI · MCP（折叠） | 在线、JSON-RPC、延迟、工具列表变更、不可用调用 | 5 个 | 未配置 MCP 时为空 |

## 指标覆盖

**本看板覆盖的指标族**：

- AI：`ai_llm_calls_total`、`ai_llm_latency_seconds`、`ai_llm_ttft_seconds`、
  `ai_llm_tokens_per_second`、`ai_llm_tokens_total{type,kind}`、
  `ai_llm_errors_total`、`ai_llm_finish_total`、`ai_llm_retries_total`、
  `ai_turn_total`、`ai_turn_duration_seconds`、`ai_turn_llm_calls`、`ai_turn_tool_calls`、
  `ai_tool_calls_total`、`ai_tool_duration_seconds`、`ai_tool_timeouts_total`、
  `ai_toolset_changes_total`、`ai_toolset_size`、`ai_tool_select_total`、
  `ai_catalog_changes_total`、`ai_catalog_generation`、`ai_tasks_created_total`、
  `ai_task_transitions_total`、`ai_approval_total`、`ai_approval_wait_seconds`、
  `ai_images_dropped_total`、`ai_images_in_request`、
  `ai_memory_extract_total`、`ai_memory_extract_facts`、`ai_memory_facts_cached`、
  `ai_rag_queries_total`、`ai_rag_duration_seconds`、
  `ai_session_active`、`ai_session_save_failures_total`、
  `ai_context_trimmed_messages_total`
- MCP（折叠行）：`mcp_server_up`、`mcp_requests_total`、`mcp_request_duration_seconds`、
  `mcp_tool_list_changes_total`、`mcp_unavailable_calls_total`
- messagelog：`records_total`、`flush_total`、`flush_failed_total`、`queue_depth`、
  `queue_dropped_total`、`spool_records`、`spool_size_bytes`、`spool_oldest_age`、
  `spool_write_failures`、`db_write_latency_seconds`、`cache_entries`、
  `cache_evictions_total`、`recorded_dropped_total`、`attachments_*`（含 status 七态、
  downloads、queue_depth、disk_usage_bytes、gc_runs/gc_deleted）
- 事件/中间件：`remilia_events_received_total{platform,kind}`、
  `remilia_events_internal_total{kind}`、
  `remilia_handler_requests_total`、`remilia_handler_latency_seconds`、
  `remilia_handler_panics_total`、`remilia_handler_timeouts_total`、
  `remilia_command_total`、`remilia_dedup_dropped_total`、`remilia_circuitbreaker_*`、
  `remilia_backpressure_dropped_total`、`remilia_ratelimit_rejected_total`、
  `remilia_ratelimit_limit`、`remilia_execpool_queue_depth`、
  `remilia_execpool_fallbacks_total`、
  `remilia_send_total`、`remilia_send_duration_seconds`、`outbound_blocked_total`、
  `outbound_requests_total`、`outbound_duration_seconds`
- 平台：`remilia_adapter_up`、`remilia_adapter_disconnects_total`、
  `remilia_adapter_fatal_errors_total`、`remilia_token_refresh_failures_total`、
  `remilia_webhook_requests_total`
- 插件加载：`remilia_plugin_load_seconds{plugin,phase}`、
  `remilia_plugin_load_failures_total{plugin,reason}`
- Minecraft（折叠行）：`minecraft_queries_total`、`minecraft_query_duration_seconds`、
  `minecraft_cache_hits_total`、`minecraft_gs4_queries_total`、
  `minecraft_rcon_commands_total`
- 运行时：`go_*`、`process_*`、`promhttp_*`、`up`、`scrape_duration_seconds`

**口径说明**（面板为空时先对照这里）：

- 出站发送指标只覆盖经 `ctx.Reply*` 发出的消息；插件直接调用
  `platform.Sender.Send`（如自带队列的直发路径）不经过观察者，不计入。
- `remilia_events_received_total` 是平台入口流量；平台标识为 `synthetic` 或为空
  的内部事件（命令执行、定时任务、计划推进）计入独立的
  `remilia_events_internal_total{kind}`，两者不混在一条曲线里。
- `ai_llm_ttft_seconds` / `ai_llm_tokens_per_second` 只对真正产出内容事件的
  流式调用采样：非流式调用观测不到首字，不写入这两个指标。
- MCP、Minecraft 等折叠行只在对应功能启用且有流量时存在；长期为空说明该功能
  未启用或没有调用。
- `kind` 维度只出现在 AI 的 token/延迟指标上；不带 `kind` 标签的时间序列会在
  按用途的面板里显示为一条无图例序列。
- `ai_turn_*` 以 `generateVerified` 为边界：校验重生成与后台计划推进都算独立回合；
  回合内的 LLM/工具次数取会话累计值的差值。
- 背压丢弃、限流拒绝、ExecPool 回退是三条不同的"未处理"路径：前者事件被丢弃，
  中者请求被拒绝，后者仍会同步执行（只是占用调用方 goroutine）。

## 常用查询

```promql
# 主对话真实命中率（按用途拆分）
sum by (kind) (rate(ai_llm_tokens_total{type="prompt_cached"}[1h]))
  / sum by (kind) (rate(ai_llm_tokens_total{type="prompt"}[1h]))

# 每轮平均输入 token（突增 = 历史/动态上下文变长）
sum(rate(ai_llm_tokens_total{type="prompt"}[15m]))
  / clamp_min(sum(rate(ai_llm_calls_total[15m])), 0.0001)

# 每 1 小时重新计费的输入 token
sum(increase(ai_llm_tokens_total{type="prompt"}[1h]))
  - sum(increase(ai_llm_tokens_total{type="prompt_cached"}[1h]))

# 首字延迟 / 生成速度（按用途）
histogram_quantile(0.95, sum by (le, kind) (rate(ai_llm_ttft_seconds_bucket[1h])))
histogram_quantile(0.50, sum by (le, kind) (rate(ai_llm_tokens_per_second_bucket[1h])))
```
