#!/usr/bin/env python3
"""生成 Remilia Bot 的 Grafana 看板 JSON（v2 排版）。

用法：
    python scripts/gen-grafana-dashboard.py deploy/grafana/remilia-bot.json

设计要点（与 v1 的 23 个全折线面板相比）：
  - 单值/状态类用 stat（up、缓存命中率、熔断状态、panic/超时、积压）
  - 分布/占比用 piechart（调用结果、附件状态、查询结果）
  - 排行/分类计数用 bargauge（命令 Top10、工具 Top10、变更原因、任务状态）
  - 直方图（LLM 延迟、工具集大小）用 heatmap，而不是折线
  - 时间序列只保留真正随时间变化的速率/分位/水位
  - 按"概览 → AI → 消息 → 事件 → 运行时 → 插件"分行，插件行默认折叠
"""

import json
import sys

DS = {"type": "prometheus", "uid": "${datasource}"}
JOB = 'job=~"$job"'

_panels = []
_next_id = 1


def _pid():
    global _next_id
    v = _next_id
    _next_id += 1
    return v


def rq(expr, legend="", instant=False, fmt=None, ref="A", interval=None):
    """构建 Prometheus target。"""
    t = {"datasource": DS, "expr": expr, "legendFormat": legend, "refId": ref}
    if instant:
        t["instant"] = True
        t["range"] = False
    if fmt:
        t["format"] = fmt
    if interval:
        t["interval"] = interval
    return t


def ts_defaults(unit="short", stack=False, fill=8, decimals=None):
    d = {
        "color": {"mode": "palette-classic"},
        "custom": {
            "axisBorderShow": False,
            "axisCenteredZero": False,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "barWidthFactor": 0.6,
            "drawStyle": "line",
            "fillOpacity": fill,
            "gradientMode": "none",
            "hideFrom": {"legend": False, "tooltip": False, "viz": False},
            "insertNulls": False,
            "lineInterpolation": "smooth",
            "lineWidth": 2,
            "pointSize": 5,
            "scaleDistribution": {"type": "linear"},
            "showPoints": "never",
            "spanNulls": False,
            "stacking": {"group": "A", "mode": "normal" if stack else "none"},
            "thresholdsStyle": {"mode": "off"},
        },
        "unit": unit,
    }
    if decimals is not None:
        d["decimals"] = decimals
    return d


def stat_defaults(unit="short", thresholds=None, mappings=None, decimals=None, minv=None, maxv=None):
    steps = thresholds or [{"color": "green", "value": None}]
    d = {
        "color": {"mode": "thresholds"},
        "mappings": mappings or [],
        "thresholds": {"mode": "absolute", "steps": steps},
        "unit": unit,
    }
    if decimals is not None:
        d["decimals"] = decimals
    if minv is not None:
        d["min"] = minv
    if maxv is not None:
        d["max"] = maxv
    return d


def pie_defaults(unit="short"):
    return {
        "color": {"mode": "palette-classic"},
        "custom": {"hideFrom": {"legend": False, "tooltip": False, "viz": False}},
        "mappings": [],
        "unit": unit,
    }


def bar_defaults(unit="short"):
    return {
        "color": {"mode": "continuous-GrYlRd"},
        "mappings": [],
        "thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": None}]},
        "unit": unit,
    }


def heat_defaults(unit="s"):
    return {
        "custom": {
            "hideFrom": {"legend": False, "tooltip": False, "viz": False},
            "scaleDistribution": {"type": "linear"},
        },
        "unit": unit,
    }


def legend(display="table", placement="bottom", calcs=("lastNotNull", "max")):
    return {
        "legend": {
            "calcs": list(calcs),
            "displayMode": display,
            "placement": placement,
            "showLegend": True,
        },
        "tooltip": {"mode": "multi", "sort": "desc"},
    }


def panel(title, ptype, targets, x, y, w, h, defaults=None, options=None, desc="", interval=None):
    p = {
        "datasource": DS,
        "description": desc,
        "fieldConfig": {"defaults": defaults or {}, "overrides": []},
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "id": _pid(),
        "options": options or {},
        "targets": targets,
        "title": title,
        "type": ptype,
    }
    if interval:
        p["interval"] = interval
    return p


def row(title, y, collapsed=False):
    return {
        "collapsed": collapsed,
        "gridPos": {"h": 1, "w": 24, "x": 0, "y": y},
        "id": _pid(),
        "panels": [],
        "title": title,
        "type": "row",
    }


def stat(title, exprs, x, y, w, h, unit="short", *, thresholds=None, mappings=None,
         decimals=None, desc="", color_mode="value", graph="none"):
    targets = []
    for i, (expr, lg) in enumerate(exprs):
        targets.append(rq(expr, lg, instant=True, ref=chr(ord("A") + i)))
    opts = {
        "colorMode": color_mode,
        "graphMode": graph,
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
        "showPercentChange": False,
        "textMode": "auto",
        "wideLayout": True,
    }
    return panel(title, "stat", targets, x, y, w, h,
                 stat_defaults(unit, thresholds, mappings, decimals), opts, desc)


def timeseries(title, exprs, x, y, w, h, unit="short", stack=False, desc="",
               display="table", placement="bottom", calcs=("lastNotNull", "max"), interval=None):
    targets = []
    for i, (expr, lg) in enumerate(exprs):
        targets.append(rq(expr, lg, ref=chr(ord("A") + i)))
    return panel(title, "timeseries", targets, x, y, w, h,
                 ts_defaults(unit, stack), legend(display, placement, calcs), desc, interval)


def pie(title, exprs, x, y, w, h, unit="short", desc="", pie_type="donut"):
    targets = []
    for i, (expr, lg) in enumerate(exprs):
        targets.append(rq(expr, lg, instant=True, ref=chr(ord("A") + i)))
    opts = {
        "legend": {"displayMode": "table", "placement": "right", "showLegend": True,
                   "values": ["value", "percent"]},
        "pieType": pie_type,
        "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
        "tooltip": {"mode": "single", "sort": "none"},
    }
    return panel(title, "piechart", targets, x, y, w, h, pie_defaults(unit), opts, desc)


def bargauge(title, exprs, x, y, w, h, unit="short", desc="", maxv=None):
    targets = []
    for i, (expr, lg) in enumerate(exprs):
        targets.append(rq(expr, lg, instant=True, ref=chr(ord("A") + i)))
    d = bar_defaults(unit)
    if maxv is not None:
        d["max"] = maxv
    opts = {
        "displayMode": "gradient",
        "legend": {"displayMode": "list", "placement": "bottom", "showLegend": False},
        "maxVizHeight": 300,
        "minVizHeight": 16,
        "minVizWidth": 8,
        "namePlacement": "left",
        "orientation": "horizontal",
        "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
        "showUnfilled": True,
        "sizing": "auto",
        "valueMode": "color",
    }
    return panel(title, "bargauge", targets, x, y, w, h, d, opts, desc)


def heatmap(title, expr, x, y, w, h, unit="s", desc=""):
    targets = [rq(expr, "{{le}}", fmt="heatmap")]
    opts = {
        "calculate": False,
        "cellGap": 1,
        "cellValues": {},
        "color": {"exponent": 0.5, "fill": "dark-orange", "mode": "scheme",
                  "reverse": False, "scale": "exponential", "scheme": "Oranges", "steps": 64},
        "exemplars": {"color": "rgba(255,0,255,0.7)"},
        "filterValues": {"le": 1e-9},
        "legend": {"show": True},
        "rowsFrame": {"layout": "auto"},
        "tooltip": {"mode": "single", "showColorScale": False, "yHistogram": False},
        "yAxis": {"axisPlacement": "left", "reverse": False, "unit": unit},
    }
    return panel(title, "heatmap", targets, x, y, w, h, heat_defaults(unit), opts, desc)


MAPPING_UP = [{
    "type": "value",
    "options": {
        "1": {"color": "green", "index": 0, "text": "UP"},
        "0": {"color": "red", "index": 1, "text": "DOWN"},
    },
}]

MAPPING_CB = [{
    "type": "value",
    "options": {
        "0": {"color": "green", "index": 0, "text": "关闭"},
        "1": {"color": "yellow", "index": 1, "text": "半开"},
        "2": {"color": "red", "index": 2, "text": "打开"},
    },
}]

HIT_RATE_EXPR = (
    'sum by (kind) (rate(ai_llm_tokens_total{job=~"$job",type="prompt_cached"}[$__rate_interval]))'
    ' / clamp_min(sum by (kind) (rate(ai_llm_tokens_total{job=~"$job",type="prompt"}[$__rate_interval])), 0.0001)'
)
HIT_RATE_TOTAL = (
    'sum(rate(ai_llm_tokens_total{job=~"$job",type="prompt_cached"}[1h]))'
    ' / clamp_min(sum(rate(ai_llm_tokens_total{job=~"$job",type="prompt"}[1h])), 0.0001)'
)

panels = []

# ── 概览 ────────────────────────────────────────────────────────────────────
panels.append(row("概览", 0))
HIT_THRESHOLDS = [
    {"color": "red", "value": None},
    {"color": "orange", "value": 0.5},
    {"color": "yellow", "value": 0.7},
    {"color": "green", "value": 0.8},
]

ov = [
    dict(title="采集状态", exprs=[('max(up{' + JOB + '})', "up")],
         unit="short", mappings=MAPPING_UP, decimals=0,
         desc="Prometheus 是否抓取到 Bot 的 /metrics。DOWN 时下面所有面板都会停更。"),
    dict(title="运行时长", exprs=[('time() - max(process_start_time_seconds{' + JOB + '})', "uptime")],
         unit="dtdurations", decimals=0,
         desc="进程启动至今（重启会让所有计数器归零）。"),
    dict(title="缓存命中率 1h", exprs=[(HIT_RATE_TOTAL, "hit")],
         unit="percentunit", thresholds=HIT_THRESHOLDS, decimals=1,
         desc="命中 token / prompt token（最近 1 小时）。DeepSeek 前缀缓存的目标水位 ≥80%。"),
    dict(title="缓存命中率 24h", exprs=[
        ('sum(increase(ai_llm_tokens_total{job=~"$job",type="prompt_cached"}[24h]))'
         ' / clamp_min(sum(increase(ai_llm_tokens_total{job=~"$job",type="prompt"}[24h])), 1)', "hit")
    ], unit="percentunit", thresholds=HIT_THRESHOLDS, decimals=1,
        desc="24 小时口径，避免短窗口抖动误判。"),
    dict(title="事件处理速率",
         exprs=[('sum(rate(remilia_handler_requests_total{' + JOB + '}[$__rate_interval]))', "events/s")],
         unit="reqps", decimals=2, desc="全事件类型合计。"),
    dict(title="未命中 token 1h", exprs=[
        ('sum(increase(ai_llm_tokens_total{' + JOB + ',type="prompt"}[1h]))'
         ' - sum(increase(ai_llm_tokens_total{' + JOB + ',type="prompt_cached"}[1h]))', "miss")
    ], unit="short", decimals=0, desc="每 1 小时重新计费的输入 token 估算。"),
    dict(title="进程内存 RSS", exprs=[('process_resident_memory_bytes{' + JOB + '}', "RSS")],
         unit="bytes", decimals=0),
    dict(title="Goroutine", exprs=[('go_goroutines{' + JOB + '}', "goroutines")],
         unit="short", decimals=0,
         thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 2000},
                     {"color": "red", "value": 8000}],
         desc="持续上涨通常是 goroutine 泄漏。"),
    dict(title="熔断器", exprs=[('remilia_circuitbreaker_state{' + JOB + '}', "state")],
         unit="short", mappings=MAPPING_CB, decimals=0,
         desc="0=关闭 / 1=半开 / 2=打开；打开说明下游失败率触顶。"),
    dict(title="Panic / 超时 1h", exprs=[
        ('sum(increase(remilia_handler_panics_total{' + JOB + '}[1h]))', "panic"),
        ('sum(increase(remilia_handler_timeouts_total{' + JOB + '}[1h]))', "timeout"),
    ], unit="short", decimals=0,
        thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}],
        desc="任何非 0 都值得看一眼日志。"),
    dict(title="出站发送 5m",
         exprs=[('sum(rate(remilia_send_total{' + JOB + ',status="success"}[5m]))', "sent/s")],
         unit="reqps", decimals=2,
         desc="经 ctx.Reply 观察到的成功发送速率（出站观察者覆盖问题修复后才有数据）。"),
    dict(title="LLM 调用 5m", exprs=[('sum(rate(ai_llm_calls_total{' + JOB + '}[5m]))', "calls/s")],
         unit="reqps", decimals=2),
]
x, y = 0, 1
for i, item in enumerate(ov):
    if i and i % 6 == 0:
        y += 5
    panels.append(stat(x=x, y=y, w=4, h=5, **item))
    x = (x + 4) % 24

# ── AI / LLM ───────────────────────────────────────────────────────────────
y += 5
panels.append(row("AI · 前缀缓存与成本", y))
y += 1
panels.append(timeseries(
    "前缀缓存命中率（按用途）",
    [(HIT_RATE_EXPR, "{{kind}}")], 0, y, 12, 8, "percentunit",
    desc="kind=chat 才是主对话的真实命中率；judge/verify/extract 等辅助调用天然无历史前缀可复用，接近 0。kind 标签自本次改动起提供，升级前的数据会显示为空序列。",
    calcs=("lastNotNull", "max")))
panels.append(timeseries(
    "Token 用量速率（按类型）",
    [('sum by (type) (rate(ai_llm_tokens_total{' + JOB + '}[$__rate_interval]))', "{{type}}")],
    12, y, 12, 8, "short",
    desc="prompt 是计费输入；prompt_cached 是其中命中缓存的部分。两者缺口 = 每轮重新计的输入。",
    calcs=("lastNotNull", "max")))
y += 8
panels.append(timeseries(
    "LLM 调用速率（按用途）",
    [('sum by (kind) (rate(ai_llm_calls_total{' + JOB + '}[$__rate_interval]))', "{{kind}}")],
    0, y, 8, 8, "reqps", desc="调用数按用途拆分；extract 占比高时整体命中率会被拉低。"))
panels.append(pie(
    "LLM 调用结果（24h）",
    [('sum by (result) (increase(ai_llm_calls_total{' + JOB + '}[24h]))', "{{result}}")],
    8, y, 8, 8, "short", "ok / error / stopped（/ai stop 或被新消息抢占）。"))
panels.append(timeseries(
    "单次调用平均输入（按用途）",
    [('sum by (kind) (rate(ai_llm_tokens_total{' + JOB + ',type="prompt"}[$__rate_interval]))'
      ' / clamp_min(sum by (kind) (rate(ai_llm_calls_total{' + JOB + '}[$__rate_interval])), 0.0001)', "{{kind}}")],
    16, y, 8, 8, "short", desc="prompt token / 调用次数。突增说明历史或动态上下文变长。"))
y += 8
panels.append(timeseries(
    "LLM 延迟分位（按模型）",
    [
        ('histogram_quantile(0.50, sum by (le, model) (rate(ai_llm_latency_seconds_bucket{' + JOB + ',model=~"$model"}[$__rate_interval])))', "p50 {{model}}"),
        ('histogram_quantile(0.95, sum by (le, model) (rate(ai_llm_latency_seconds_bucket{' + JOB + ',model=~"$model"}[$__rate_interval])))', "p95 {{model}}"),
        ('histogram_quantile(0.99, sum by (le, model) (rate(ai_llm_latency_seconds_bucket{' + JOB + ',model=~"$model"}[$__rate_interval])))', "p99 {{model}}"),
    ], 0, y, 12, 8, "s", desc="流式调用的耗时覆盖整段消费时长。"))
panels.append(heatmap(
    "LLM 延迟分布",
    'sum by (le) (rate(ai_llm_latency_seconds_bucket{' + JOB + ',model=~"$model"}[$__rate_interval]))',
    12, y, 12, 8, "s", "延迟分布。长尾（>30s）集中在哪一档一眼可见，p95 会掩盖双峰。"))
y += 8
panels.append(row("AI · 首字延迟与吞吐", y))
y += 1
panels.append(timeseries(
    "首字延迟 p50/p95（按用途）",
    [
        ('histogram_quantile(0.50, sum by (le, kind) (rate(ai_llm_ttft_seconds_bucket{' + JOB + '}[$__rate_interval])))', "p50 {{kind}}"),
        ('histogram_quantile(0.95, sum by (le, kind) (rate(ai_llm_ttft_seconds_bucket{' + JOB + '}[$__rate_interval])))', "p95 {{kind}}"),
    ], 0, y, 12, 8, "s",
    desc="发起请求 → 第一个内容事件（正文/推理/工具调用）。上升说明排队/容量或上游拥塞；与生成速度分开看才能区分“慢在等”和“慢在写”。"))
panels.append(timeseries(
    "生成速度 p50/p95（按用途）",
    [
        ('histogram_quantile(0.50, sum by (le, kind) (rate(ai_llm_tokens_per_second_bucket{' + JOB + '}[$__rate_interval])))', "p50 {{kind}}"),
        ('histogram_quantile(0.95, sum by (le, kind) (rate(ai_llm_tokens_per_second_bucket{' + JOB + '}[$__rate_interval])))', "p95 {{kind}}"),
    ], 12, y, 12, 8, "short",
    desc="completion token ÷（首字 → 流结束），不含排队与 prefill；单位 tok/s。只对产出内容事件的流式调用采样。"))
y += 8
panels.append(timeseries(
    "推理 token 占比",
    [('sum(rate(ai_llm_tokens_total{' + JOB + ',type="reasoning"}[$__rate_interval]))'
      ' / clamp_min(sum(rate(ai_llm_tokens_total{' + JOB + ',type="completion"}[$__rate_interval])), 0.0001)', "reasoning/completion")],
    0, y, 8, 8, "percentunit",
    desc="接近 1 说明思考过程吃掉了大部分输出预算（正文容易为空或被截断）。"))
panels.append(bargauge(
    "工具集变更（24h，按原因）",
    [('sum by (reason) (increase(ai_toolset_changes_total{' + JOB + '}[24h]))', "{{reason}}")],
    8, y, 8, 8, "short",
    desc="init/grow/shrink/decay。每次 grow/shrink 都会让 tools 及其后的历史前缀缓存失效，init 只应出现在新会话。"))
panels.append(stat(
    "工具集规模（中位）",
    [('histogram_quantile(0.5, sum by (le) (rate(ai_toolset_size_bucket{' + JOB + '}[$__rate_interval])))', "median")],
    16, y, 8, 8, "short", decimals=0,
    desc="每轮发送的工具数量中位数，与 tool_select_max / tool_budget 对照。"))
y += 8
panels.append(heatmap(
    "工具集大小分布",
    'sum by (le) (rate(ai_toolset_size_bucket{' + JOB + '}[$__rate_interval]))',
    0, y, 8, 8, "short", "工具集大小分布：tools 排在最前，集合越大固定前缀越长、越难整体命中。"))
panels.append(bargauge(
    "目录变更（24h，按原因）",
    [('sum by (reason) (increase(ai_catalog_changes_total{' + JOB + '}[24h]))', "{{reason}}")],
    8, y, 8, 8, "short", "add/remove/update。每次推进都会让会话级工具选择缓存失效。"))
panels.append(stat(
    "目录代数",
    [('max(ai_catalog_generation{' + JOB + '})', "generation")], 16, y, 8, 8, "short", decimals=0,
    desc="当前目录内容版本；与上面的变更速率一起看可判断是否在高频抖动。"))
y += 8
panels.append(bargauge(
    "任务状态迁移（24h）",
    [('sum by (status) (increase(ai_task_transitions_total{' + JOB + '}[24h]))', "{{status}}")],
    0, y, 8, 8, "short", "任务进入各状态的次数（running/succeeded/failed/cancelled/interrupted）。"))
panels.append(stat(
    "任务创建 24h",
    [('sum(increase(ai_tasks_created_total{' + JOB + '}[24h]))', "created")], 8, y, 8, 8, "short", decimals=0,
    desc="新计划取代旧任务同样计数。"))
panels.append(bargauge(
    "工具调用 Top10（24h）",
    [('topk(10, sum by (tool, result) (increase(ai_tool_calls_total{' + JOB + '}[24h])))', "{{tool}} / {{result}}")],
    16, y, 8, 8, "short", "按工具的调用次数排行；error 占比高的工具值得单独看。"))

# ── AI · 回合质量与失败 ────────────────────────────────────────────────────
y += 8
panels.append(row("AI · 回合质量与失败", y))
y += 1
panels.append(bargauge(
    "回合结果（24h）",
    [('sum by (result) (increase(ai_turn_total{' + JOB + '}[24h]))', "{{result}}")],
    0, y, 8, 8, "short",
    "ok/empty/error/interrupted。命中率之外，这一栏回答“这一轮到底有没有答上”。"))
panels.append(timeseries(
    "回合耗时 p50/p95",
    [
        ('histogram_quantile(0.50, sum by (le) (rate(ai_turn_duration_seconds_bucket{' + JOB + '}[$__rate_interval])))', "p50"),
        ('histogram_quantile(0.95, sum by (le) (rate(ai_turn_duration_seconds_bucket{' + JOB + '}[$__rate_interval])))', "p95"),
    ], 8, y, 8, 8, "s", desc="用户可感知的完整等待时长（含工具轮与校验重生成）。"))
panels.append(timeseries(
    "回合内调用数 p95",
    [
        ('histogram_quantile(0.95, sum by (le) (rate(ai_turn_llm_calls_bucket{' + JOB + '}[$__rate_interval])))', "LLM 调用"),
        ('histogram_quantile(0.95, sum by (le) (rate(ai_turn_tool_calls_bucket{' + JOB + '}[$__rate_interval])))', "工具调用"),
    ], 16, y, 8, 8, "short", desc="回合规模。撞 max_depth 或工具连环失败时，这里会先出现异常。"))
y += 8
panels.append(pie(
    "结束原因（24h）",
    [('sum by (reason) (increase(ai_llm_finish_total{' + JOB + '}[24h]))', "{{reason}}")],
    0, y, 8, 8, "short",
    "stop/length/content_filter/tool_calls。length 占比高说明输出预算不够，content_filter 高说明被安全策略截断。"))
panels.append(bargauge(
    "失败分类（24h）",
    [('sum by (class) (increase(ai_llm_errors_total{' + JOB + '}[24h]))', "{{class}}")],
    8, y, 8, 8, "short",
    "429=rate_limit、401/403=auth、400+context_length=上下文超长、5xx=server。四类处置完全不同。"))
panels.append(timeseries(
    "工具耗时 p95（Top5）",
    [('topk(5, histogram_quantile(0.95, sum by (le, tool) (rate(ai_tool_duration_seconds_bucket{' + JOB + '}[$__rate_interval]))))', "{{tool}}")],
    16, y, 8, 8, "s", desc="工具是回合里最慢的一段；超时与业务失败分开看 ai_tool_timeouts_total。"))
y += 8
panels.append(stat(
    "工具超时 / 审批超时 24h",
    [
        ('sum(increase(ai_tool_timeouts_total{' + JOB + '}[24h]))', "工具超时"),
        ('sum(increase(ai_approval_total{' + JOB + ',result="timeout"}[24h]))', "审批超时"),
    ], 0, y, 8, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}], decimals=0,
    desc="超时说明预算不够或工具/审批卡住，不是业务失败。"))
panels.append(pie(
    "审批结果（24h）",
    [('sum by (result) (increase(ai_approval_total{' + JOB + '}[24h]))', "{{result}}")],
    8, y, 8, 8, "short", "approved/denied/timeout/interrupted。"))
panels.append(stat(
    "记忆抽取 / RAG 24h",
    [
        ('sum(increase(ai_memory_extract_total{' + JOB + ',result="ok"}[24h]))', "抽取成功"),
        ('sum(increase(ai_memory_extract_total{' + JOB + ',result="error"}[24h]))', "抽取失败"),
        ('sum(increase(ai_rag_queries_total{' + JOB + ',result="hit"}[24h]))', "RAG 命中"),
    ], 16, y, 8, 8, "short", decimals=0,
    desc="抽取失败不应被 Debug 日志埋掉；RAG 长期 0 命中说明检索没起作用。"))

# ── 消息记录与附件 ─────────────────────────────────────────────────────────
y += 8
panels.append(row("消息记录与附件（messagelog）", y))
y += 1
panels.append(timeseries(
    "记录 / 落盘速率",
    [
        ('rate(messagelog_records_total{' + JOB + '}[$__rate_interval])', "记录"),
        ('rate(messagelog_flush_total{' + JOB + '}[$__rate_interval])', "落盘事务"),
        ('rate(messagelog_flush_failed_total{' + JOB + '}[$__rate_interval])', "落盘失败"),
    ], 0, y, 8, 8, "reqps"))
panels.append(timeseries(
    "队列与 spool 积压",
    [
        ('messagelog_queue_depth{' + JOB + '}', "内存队列"),
        ('messagelog_spool_records{' + JOB + '}', "spool 未确认"),
    ], 8, y, 8, 8, "short", desc="spool 有积压说明 SQLite 落盘跟不上写入。"))
panels.append(stat(
    "最旧积压（秒）",
    [('messagelog_spool_oldest_age{' + JOB + '}', "oldest")], 16, y, 8, 8, "s",
    thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 60},
                {"color": "red", "value": 300}], decimals=0,
    desc="spool 最旧未确认记录的年龄，超过 5 分钟要查落盘。"))
y += 8
panels.append(stat(
    "丢弃 / 写失败 1h",
    [
        ('sum(increase(messagelog_queue_dropped_total{' + JOB + '}[1h]))', "队列丢弃"),
        ('sum(increase(messagelog_recorded_dropped_total{' + JOB + '}[1h]))', "记录丢弃"),
        ('sum(increase(messagelog_spool_write_failures{' + JOB + '}[1h]))', "spool 写失败"),
    ], 0, y, 8, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}], decimals=0,
    desc="开启 spool 后“队列丢弃”应恒为 0；非 0 即丢消息。"))
panels.append(timeseries(
    "SQLite 写延迟 p95",
    [('histogram_quantile(0.95, sum by (le) (rate(messagelog_db_write_latency_seconds_bucket{' + JOB + '}[$__rate_interval])))', "p95")],
    8, y, 8, 8, "s"))
panels.append(stat(
    "热缓存 / 淘汰 1h",
    [
        ('messagelog_cache_entries{' + JOB + '}', "缓存条目"),
        ('sum(increase(messagelog_cache_evictions_total{' + JOB + '}[1h]))', "淘汰 1h"),
    ], 16, y, 8, 8, "short", decimals=0, desc="热缓存被频繁淘汰会让最近消息查询退化到 SQLite。"))
y += 8
panels.append(pie(
    "附件状态分布",
    [
        ('messagelog_attachments_status_ready{' + JOB + '}', "就绪"),
        ('messagelog_attachments_status_pending{' + JOB + '} + messagelog_attachments_status_pending_retry{' + JOB + '} + messagelog_attachments_status_pending_lazy{' + JOB + '}', "待处理"),
        ('messagelog_attachments_status_failed{' + JOB + '}', "失败"),
        ('messagelog_attachments_status_expired{' + JOB + '} + messagelog_attachments_status_deleted{' + JOB + '}', "过期/删除"),
    ], 0, y, 8, 8, "short"))
panels.append(timeseries(
    "附件下载速率",
    [
        ('rate(messagelog_attachments_downloads_total{' + JOB + '}[$__rate_interval])', "尝试"),
        ('rate(messagelog_attachments_downloads_ready_total{' + JOB + '}[$__rate_interval])', "成功"),
        ('rate(messagelog_attachments_downloads_failed_total{' + JOB + '}[$__rate_interval])', "失败"),
    ], 8, y, 8, 8, "reqps"))
panels.append(stat(
    "附件失败率 1h",
    [('sum(increase(messagelog_attachments_downloads_failed_total{' + JOB + '}[1h]))'
      ' / clamp_min(sum(increase(messagelog_attachments_downloads_total{' + JOB + '}[1h])), 1)', "fail")],
    16, y, 8, 8, "percentunit",
    thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 0.05},
                {"color": "red", "value": 0.2}], decimals=1))
y += 8
panels.append(timeseries(
    "附件磁盘占用",
    [
        ('messagelog_attachments_disk_usage_bytes{' + JOB + '}', "磁盘占用"),
        ('messagelog_spool_size_bytes{' + JOB + '}', "spool 文件"),
    ], 0, y, 8, 8, "bytes"))
panels.append(stat(
    "下载队列深度",
    [('messagelog_attachments_queue_depth{' + JOB + '}', "queue")], 8, y, 8, 8, "short", decimals=0,
    desc="持续 >0 说明下载吞吐跟不上入站图片。"))
panels.append(stat(
    "GC 清理 24h",
    [
        ('sum(increase(messagelog_attachments_gc_runs_total{' + JOB + '}[24h]))', "GC 次数"),
        ('sum(increase(messagelog_attachments_gc_deleted_total{' + JOB + '}[24h]))', "删除文件"),
    ], 16, y, 8, 8, "short", decimals=0, desc="过期附件清理是否真的在跑。"))

# ── 事件与平台 ─────────────────────────────────────────────────────────────
y += 8
panels.append(row("事件与平台", y))
y += 1
panels.append(timeseries(
    "事件入口速率（按平台/类型）",
    [('sum by (platform, kind) (rate(remilia_events_received_total{' + JOB + '}[$__rate_interval]))', "{{platform}}/{{kind}}")],
    0, y, 6, 8, "reqps",
    desc="进入引擎处理的平台事件（不含内部事件）。与下一栏对照可看出外部流量与插件自造流量的比例。",
    display="list"))
panels.append(timeseries(
    "内部事件速率（按类型）",
    [('sum by (kind) (rate(remilia_events_internal_total{' + JOB + '}[$__rate_interval]))', "{{kind}}")],
    6, y, 6, 8, "reqps",
    desc="命令执行、定时任务、计划推进等由插件构造的事件（platform=synthetic 或未绑定平台）。同样占用处理能力，但与平台入口流量分开统计。",
    display="list"))
panels.append(timeseries(
    "事件处理速率（按类型）",
    [('sum by (event) (rate(remilia_handler_requests_total{' + JOB + '}[$__rate_interval]))', "{{event}}")],
    12, y, 6, 8, "reqps", display="list"))
panels.append(timeseries(
    "Handler 延迟 p95（按类型）",
    [('histogram_quantile(0.95, sum by (le, event) (rate(remilia_handler_latency_seconds_bucket{' + JOB + '}[$__rate_interval])))', "{{event}}")],
    18, y, 6, 8, "s", display="list"))
y += 8
panels.append(bargauge(
    "命令使用 Top10（24h）",
    [('topk(10, sum by (command) (increase(remilia_command_total{' + JOB + '}[24h])))', "{{command}}")],
    0, y, 8, 8, "short"))
panels.append(timeseries(
    "去重丢弃速率",
    [('sum by (platform) (rate(remilia_dedup_dropped_total{' + JOB + '}[$__rate_interval]))', "{{platform}}")],
    8, y, 8, 8, "reqps", desc="平台重推/重复投递被丢弃的速率，突增说明上游在重试。"))
panels.append(stat(
    "熔断打开次数 24h",
    [
        ('sum(increase(remilia_circuitbreaker_trips_total{' + JOB + '}[24h]))', "trips"),
        ('sum(increase(remilia_circuitbreaker_rejected_total{' + JOB + '}[24h]))', "rejected"),
    ], 16, y, 8, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}], decimals=0,
    desc="trips = 熔断触发次数；rejected = 熔断期间被拒绝的请求数。"))
y += 8
panels.append(timeseries(
    "出站发送速率（按平台/结果）",
    [('sum by (platform, status) (rate(remilia_send_total{' + JOB + '}[$__rate_interval]))', "{{platform}}/{{status}}")],
    0, y, 8, 8, "reqps",
    desc="只统计经 ctx.Reply* 发出的消息（绕过 Reply 的直发不计）。"))
panels.append(timeseries(
    "被出站策略拦截（按组件/原因）",
    [('sum by (component, reason) (rate(outbound_blocked_total{' + JOB + '}[$__rate_interval]))', "{{component}} / {{reason}}")],
    8, y, 8, 8, "reqps",
    desc="v1.76 起：用户/模型给出的 URL 被 SSRF/来源策略拦下的次数。持续非 0 说明功能被策略误伤或有真实攻击尝试。"))
panels.append(timeseries(
    "发送延迟 p95（按平台）",
    [('histogram_quantile(0.95, sum by (le, platform) (rate(remilia_send_duration_seconds_bucket{' + JOB + '}[$__rate_interval])))', "{{platform}}")],
    16, y, 8, 8, "s",
    desc="平台 Send 的往返耗时（不含队列等待）；与 Handler 延迟对照可区分“模型慢”还是“平台慢”。"))

# ── AI · 上下文维护 / 事件管线 / 平台健康 / 消息链路延迟 ──────────────────
y += 8
panels.append(row("AI · 上下文维护", y))
y += 1
panels.append(stat(
    "会话数 / 裁剪消息 24h",
    [
        ('max(ai_session_active{' + JOB + '})', "活跃会话"),
        ('sum(increase(ai_context_trimmed_messages_total{' + JOB + '}[24h]))', "裁剪消息"),
    ], 0, y, 8, 8, "short", decimals=0,
    desc="裁剪是前缀缓存的主要断点：裁剪条数与命中率曲线对照，能区分“窗口太小”与“缓存结构不好”。"))
panels.append(bargauge(
    "图片剥离（24h，按原因）",
    [('sum by (reason) (increase(ai_images_dropped_total{' + JOB + '}[24h]))', "{{reason}}")],
    8, y, 8, 8, "short",
    "stale=保留窗口/时间窗过期，budget=单请求预算超限。图片剥离同时影响成本与历史字节一致性。"))
panels.append(pie(
    "工具选择路径（24h）",
    [('sum by (path) (increase(ai_tool_select_total{' + JOB + '}[24h]))', "{{path}}")],
    16, y, 8, 8, "short",
    "bypass=工具数未超阈值或无查询，retrieval=本地检索 Top-K。长期全 bypass 说明阈值/嵌入没生效。"))
y += 8
panels.append(stat(
    "记忆事实（缓存口径）",
    [('max(ai_memory_facts_cached{' + JOB + '})', "facts")], 0, y, 8, 8, "short", decimals=0,
    desc="已加载作用域内的事实总数；持续增长到 memory_max_facts 附近说明淘汰在生效。"))

y += 8
panels.append(row("事件管线 · 丢弃与背压", y))
y += 1
panels.append(timeseries(
    "背压丢弃（按策略）",
    [('sum by (policy) (rate(remilia_backpressure_dropped_total{' + JOB + '}[$__rate_interval]))', "{{policy}}")],
    0, y, 6, 8, "reqps",
    desc="背压是唯一会静默丢事件的中间件：非 0 说明事件没进 handler。",
    display="list"))
panels.append(timeseries(
    "限流拒绝速率",
    [('rate(remilia_ratelimit_rejected_total{' + JOB + '}[$__rate_interval])', "{{reason}}")],
    6, y, 6, 8, "reqps",
    desc="自适应限流拒绝的请求（limit=并发达上限，acquire=抢占失败）。",
    display="list"))
panels.append(stat(
    "当前限流上限",
    [('max(remilia_ratelimit_limit{' + JOB + '})', "limit")], 12, y, 4, 8, "short", decimals=0,
    desc="自适应限流当前生效的并发上限，跟踪它的调节轨迹。"))
panels.append(stat(
    "ExecPool 回退 1h",
    [('sum(increase(remilia_execpool_fallbacks_total{' + JOB + '}[1h]))', "fallback")],
    16, y, 4, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}], decimals=0,
    desc="池满回退同步执行：事件仍被处理，但占用了调用方 goroutine，延迟会上升。"))
panels.append(timeseries(
    "ExecPool 队列深度",
    [('remilia_execpool_queue_depth{' + JOB + '}', "queue")], 20, y, 4, 8, "short"))

y += 8
panels.append(row("平台与出站健康", y))
y += 1
panels.append(stat(
    "适配器状态",
    [('remilia_adapter_up{' + JOB + '}', "{{platform}}")], 0, y, 4, 8, "short",
    thresholds=[{"color": "red", "value": None}, {"color": "green", "value": 1}], decimals=0,
    desc="1=运行中 / 0=已停止。为 0 时进程还活着，但该平台已收不到消息。"))
panels.append(stat(
    "断连 / 致命错误 24h",
    [
        ('sum(increase(remilia_adapter_disconnects_total{' + JOB + '}[24h]))', "断连"),
        ('sum(increase(remilia_adapter_fatal_errors_total{' + JOB + '}[24h]))', "致命"),
    ], 4, y, 4, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}], decimals=0))
panels.append(stat(
    "Token 刷新失败 24h",
    [('sum(increase(remilia_token_refresh_failures_total{' + JOB + '}[24h]))', "failures")],
    8, y, 4, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}], decimals=0,
    desc="刷新失败不会让进程退出，但会让平台 API 调用全部失败。"))
panels.append(stat(
    "Webhook 4xx 24h",
    [('sum(increase(remilia_webhook_requests_total{status=~"4.."}[24h]))', "4xx")],
    12, y, 4, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 1}], decimals=0,
    desc="401 通常意味着签名密钥不一致；429 是上游限流。"))
panels.append(timeseries(
    "出站 HTTP 速率（按结果）",
    [('sum by (component, result) (rate(outbound_requests_total{' + JOB + '}[$__rate_interval]))', "{{component}}/{{result}}")],
    16, y, 8, 8, "reqps", display="list"))

y += 8
panels.append(row("消息链路延迟", y))
y += 1
panels.append(timeseries(
    "消息查询 p95（按来源）",
    [('histogram_quantile(0.95, sum by (le, source) (rate(messagelog_query_duration_seconds_bucket{' + JOB + '}[$__rate_interval])))', "{{source}}")],
    0, y, 12, 8, "s", desc="source=memory 是热缓存，sqlite 是回源；后者变慢会拖慢每轮上下文构建。"))
panels.append(timeseries(
    "附件下载 p95（按结果）",
    [('histogram_quantile(0.95, sum by (le, result) (rate(messagelog_attachments_download_duration_seconds_bucket{' + JOB + '}[$__rate_interval])))', "{{result}}")],
    12, y, 12, 8, "s"))

y += 8
panels.append(row("插件加载", y))
y += 1
panels.append(timeseries(
    "插件加载耗时 p95（按阶段）",
    [('histogram_quantile(0.95, sum by (le, phase) (rate(remilia_plugin_load_seconds_bucket{' + JOB + '}[$__rate_interval])))', "{{phase}}")],
    0, y, 12, 8, "s",
    desc="config=配置构造与校验，setup=Setup 与 matcher 注册。启动慢时先看哪个阶段。"))
panels.append(bargauge(
    "加载失败（24h，按原因）",
    [('sum by (reason) (increase(remilia_plugin_load_failures_total{' + JOB + '}[24h]))', "{{reason}}")],
    12, y, 12, 8, "short",
    "config/setup/timeout/panic。panic 与超时的处置不同：前者是插件 bug，后者是启动预算不够。"))

# ── 进程与运行时 ───────────────────────────────────────────────────────────
y += 8
panels.append(row("进程与 Go 运行时", y))
y += 1
panels.append(timeseries(
    "CPU 使用率",
    [('rate(process_cpu_seconds_total{' + JOB + '}[$__rate_interval])', "cpu")], 0, y, 8, 7, "percentunit"))
panels.append(timeseries(
    "内存（RSS / Heap）",
    [
        ('process_resident_memory_bytes{' + JOB + '}', "RSS"),
        ('go_memstats_heap_inuse_bytes{' + JOB + '}', "heap inuse"),
        ('go_memstats_heap_released_bytes{' + JOB + '}', "heap released"),
    ], 8, y, 8, 7, "bytes"))
panels.append(timeseries(
    "Goroutine / OS 线程",
    [
        ('go_goroutines{' + JOB + '}', "goroutines"),
        ('go_threads{' + JOB + '}', "threads"),
    ], 16, y, 8, 7, "short"))
y += 7
panels.append(timeseries(
    "GC 暂停（最大）",
    [('go_gc_duration_seconds{' + JOB + ',quantile="1"}', "pause")], 0, y, 8, 7, "s",
    desc="Go runtime 的 GC 停顿分位（summary 自带 0/0.25/0.5/0.75/1）。"))
panels.append(timeseries(
    "文件描述符",
    [
        ('process_open_fds{' + JOB + '}', "open"),
        ('process_max_fds{' + JOB + '}', "max"),
    ], 8, y, 8, 7, "short"))
panels.append(timeseries(
    "Prometheus 抓取耗时",
    [('scrape_duration_seconds{' + JOB + '}', "scrape")], 16, y, 8, 7, "s",
    desc="抓取本身变慢会影响指标精度（默认 30s 间隔）。"))

# ── Minecraft（折叠） ──────────────────────────────────────────────────────
y += 7
panels.append(row("插件 · Minecraft", y, collapsed=True))
row_mc = panels[-1]
y += 1
mc_panels = [
    timeseries("查询速率（按途径）",
               [('sum by (via) (rate(minecraft_queries_total{' + JOB + '}[$__rate_interval]))', "{{via}}")],
               0, y, 12, 8, "reqps"),
    pie("查询结果（24h）",
        [('sum by (result) (increase(minecraft_queries_total{' + JOB + '}[24h]))', "{{result}}")],
        12, y, 12, 8, "short"),
    timeseries("p95 查询延迟（按版本）",
               [('histogram_quantile(0.95, sum by (le, edition) (rate(minecraft_query_duration_seconds_bucket{' + JOB + '}[$__rate_interval])))', "{{edition}}")],
               0, y + 8, 12, 8, "s"),
    stat("缓存命中率 24h",
         [('sum(increase(minecraft_cache_hits_total{' + JOB + '}[24h]))'
           ' / clamp_min(sum(increase(minecraft_queries_total{' + JOB + '}[24h])), 1)', "hit")],
         12, y + 8, 4, 8, "percentunit",
         thresholds=[{"color": "red", "value": None}, {"color": "yellow", "value": 0.3},
                     {"color": "green", "value": 0.6}], decimals=1),
    bargauge("GS4 / RCON（24h）",
             [('sum by (result) (increase(minecraft_gs4_queries_total{' + JOB + '}[24h]))', "gs4/{{result}}"),
              ('sum by (command) (increase(minecraft_rcon_commands_total{' + JOB + '}[24h]))', "rcon/{{command}}")],
             16, y + 8, 8, 8, "short"),
]
row_mc["panels"] = mc_panels

# ── MCP（折叠，未启用时为空） ──────────────────────────────────────────────
y += 16
panels.append(row("AI · MCP 外部工具服务器（未配置时为空）", y, collapsed=True))
row_mcp = panels[-1]
y += 1
row_mcp["panels"] = [
    stat("服务器在线",
         [('mcp_server_up{' + JOB + '}', "{{server}}")], 0, y, 8, 8, "short",
         decimals=0, desc="1=已连接 / 0=未连接。"),
    timeseries("JSON-RPC 速率（按方法/结果）",
               [('sum by (method, result) (rate(mcp_requests_total{' + JOB + '}[$__rate_interval]))', "{{method}}/{{result}}")],
               8, y, 16, 8, "reqps"),
    timeseries("请求延迟 p95（按服务器）",
               [('histogram_quantile(0.95, sum by (le, server) (rate(mcp_request_duration_seconds_bucket{' + JOB + '}[$__rate_interval])))', "{{server}}")],
               0, y + 8, 12, 8, "s"),
    bargauge("工具列表变更（24h）",
             [('sum by (result) (increase(mcp_tool_list_changes_total{' + JOB + '}[24h]))', "{{result}}")],
             12, y + 8, 6, 8, "short",
             desc="adopted=真实变更并推进目录代际；jitter=被稳定策略吸收的抖动。"),
    stat("不可用调用 24h",
         [('sum(increase(mcp_unavailable_calls_total{' + JOB + '}[24h]))', "unavailable")],
         18, y + 8, 6, 8, "short", thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}], decimals=0,
         desc="连接断开期间模型请求的工具数（应被稳定策略抵扣）。"),
]

dashboard = {
    "annotations": {
        "list": [{
            "builtIn": 1,
            "datasource": {"type": "grafana", "uid": "-- Grafana --"},
            "enable": True,
            "hide": True,
            "iconColor": "rgba(0, 211, 255, 1)",
            "name": "Annotations & Alerts",
            "type": "dashboard",
        }]
    },
    "editable": True,
    "graphTooltip": 1,
    "links": [],
    "panels": panels,
    "refresh": "30s",
    "schemaVersion": 39,
    "tags": ["remilia", "bot", "ai"],
    "templating": {
        "list": [
            {
                "current": {"selected": True, "text": "Prometheus", "value": "cfxbcp7ek579cd"},
                "hide": 0,
                "includeAll": False,
                "label": "数据源",
                "multi": False,
                "name": "datasource",
                "options": [],
                "query": "prometheus",
                "refresh": 1,
                "regex": "",
                "skipUrlSync": False,
                "type": "datasource",
            },
            {
                "current": {"selected": True, "text": "remilia-bot", "value": "remilia-bot"},
                "definition": "label_values(up, job)",
                "hide": 0,
                "includeAll": False,
                "label": "Job",
                "multi": False,
                "name": "job",
                "options": [],
                "query": {"query": "label_values(up, job)", "refId": "StandardVariableQuery"},
                "refresh": 2,
                "regex": "",
                "skipUrlSync": False,
                "sort": 1,
                "type": "query",
            },
            {
                "current": {"selected": True, "text": "All", "value": "$__all"},
                "datasource": DS,
                "definition": "label_values(ai_llm_calls_total{job=~\"$job\"}, model)",
                "hide": 0,
                "includeAll": True,
                "allValue": ".*",
                "label": "模型",
                "multi": True,
                "name": "model",
                "options": [],
                "query": {"query": "label_values(ai_llm_calls_total{job=~\"$job\"}, model)", "refId": "StandardVariableQuery"},
                "refresh": 2,
                "regex": "",
                "skipUrlSync": False,
                "sort": 1,
                "type": "query",
            },
        ]
    },
    "time": {"from": "now-6h", "to": "now"},
    "timepicker": {},
    "timezone": "browser",
    "title": "Remilia Bot",
    "uid": "57ab5c4a-525b-4215-afb9-1155dfdc4d92",
    "version": 1,
}

if len(sys.argv) > 1:
    with open(sys.argv[1], "w", encoding="utf-8") as f:
        json.dump(dashboard, f, ensure_ascii=False, indent=2)
        f.write("\n")
else:
    print(json.dumps(dashboard, ensure_ascii=False, indent=2))


