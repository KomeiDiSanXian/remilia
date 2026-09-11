# AI 对话插件指南

内置 `ai` 插件（`builtin/ai/`）提供多提供商（OpenAI / Anthropic）的 AI 对话、工具调用与自定义技能管理。

## 触发方式

- **@机器人** 后直接发消息（`at_bot: true`，默认）
- **私聊**（`private_chat: true`）
- **群聊自主发言**：不 @ 机器人也响应群内非命令消息（`group_autonomous: true`）
- **无命令匹配兜底**：群聊与私聊的全部非命令消息都由 AI 应答（`fallback: true`，
  即 `group_autonomous` + `private_chat` 的并集；命令消息仍归其他插件）
- 消息前缀触发：`trigger_cmd`（默认 `/ai`）

## 命令

| 命令 | 说明 |
|------|------|
| `/ai <消息>` | 与 AI 对话（支持工具调用与图片） |
| `/ai reset` | 清空当前会话对话历史 |
| `/ai undo` | 撤销上一条对话 |
| `/ai retry` | 重新生成上一条回复 |
| `/ai stop` | 停止当前正在生成的回复（@机器人 说"停止"亦可） |
| `/ai summary` | 后台生成对话总结 |
| `/ai status` | 查看会话状态（提供商/模型/消息数/时长） |
| `/ai stats` | 查看使用统计（LLM 调用次数、工具调用次数） |
| `/ai tools` | 列出当前可用工具 |
| `/ai memory` | 查看长期记忆；`remove <序号\|文本>` 删除单条/按内容删，`clear [user\|group]` 清空（`clear group` 需群管理员） |
| `/ai todo` | 管理会话待办清单（list / add / done / remove / clear） |
| `/ai plan` | 查看/取消当前任务计划（status / cancel） |
| `/ai skill ...` | 自定义技能管理（见下） |

> **关于 QQ"重新生成 / 清空会话 / 停止生成"按钮**：QQ v2 公开文档只提供挂在
> markdown 消息上的 keyboard 按钮（见官方《消息按钮》），`action_button`（含
> "重新生成/停止生成"模板）无公开文档：真机冒烟确认模板 "1" 只渲染"赞/踩"
> 反馈行、模板 "10" 直接被服务端拒绝（HTTP 400）。操作按钮使用官方 keyboard
> **指令按钮**（`action.type=2`，与 about 插件"查看命令列表"同方案）：QQ 单聊
> 与群聊（频道除外）的 Markdown 回复底部显示"重新生成"与"清空会话"，点击后
> 客户端把命令填入输入框（群聊自动补 `@bot` 前缀，见官方 action.type=2 说明）
> ——单聊手机端（客户端 8983+，Enter=true 仅单聊生效）自动发送，桌面端与
> 群聊需手动按回车发送；"清空会话"不自动发送（破坏性动作，避免误触）。
> 不用回调按钮（type=1）的原因：QQ webhook 下互动事件投递有秒级延迟（真机
> 实测约 2s），即使服务端
> 成功回应，客户端仍显示"请求第三方失败"（2026-09 真机验证，与 about 现象
> 一致）；指令按钮不产生互动回调，命令走常规文本消息管线，可靠且可被新消息
> 抢占。连点/重复触发受会话级冷却与忙时节流提示保护。QQ 客户端自带的"清空
> 会话"入口（官方原生互动 type=14，CLEAR_SESSION）也会同步清空本插件会话。
> 纯文本回复、富媒体回复、频道消息不附加按钮，请使用 `/ai retry`、`/ai reset`
> 命令。
>
> 复杂任务触发计划（create_plan）时推送的"计划已创建"消息、以及 `/ai plan`
> 状态回复同样会附加指令按钮："查看计划"（`/ai plan`，单聊手机端自动发送，
> 长任务执行期间一键刷新进度）与"停止生成"（`/ai stop`，不自动发送，仅在
> 回合进行中时附带）。停止生成会一并取消进行中的任务计划——半途中断后若不
> 取消，剩余步骤仍会在后续回合/后台自动推进（`/ai stop` 与 `/ai plan cancel`
> 语义一致）。按钮出现在 QQ 单聊与群聊（频道除外）的 Markdown 消息上（受
> `qq_plan_button` 开关控制）。
>
> QQ 的原生"停止生成"按钮只在流式消息渲染期间可用，本插件以整条发送为主，
> 因此未接入该按钮；停止统一走 `/ai stop`（文本命令或计划消息上的"停止生成"
> 指令按钮，中断进行中的 LLM 流，已生成部分会保留），全平台可用；用户直接
> 发送新消息同样会抢占并中断上一轮生成。

## 配置（`plugins.ai`）

```yaml
plugins:
  ai:
    provider: "openai"            # openai（默认）| anthropic
    model: ""                     # 模型名（空 = 提供商默认）
    base_url: ""                  # 兼容端点（OpenAI 兼容 API 可指向代理/中转）
    api_key: ""
    max_tokens: 0                 # 单次回复最大 token
    max_depth: 0                  # 工具调用最大轮数
    max_history: 0                # 保留的历史消息数
    api_timeout: ""               # LLM API 超时
    tool_timeout: ""              # 工具执行超时
    session_ttl: ""               # 会话存活时间（过期自动清理）
    system_prompt: ""             # 系统提示词
    trigger_cmd: "/ai"            # 命令触发前缀
    at_bot: true                  # 允许 @机器人 触发
    private_chat: true            # 允许私聊触发
    group_autonomous: false       # 群聊自主发言：不 @ 机器人也响应群内非命令消息
    markdown: true                # 回复使用 Markdown
    qq_regen_button: true         # QQ 单聊/群聊（频道除外）AI Markdown 回复附加"重新生成"指令按钮（type=2，单聊手机端自动发送 /ai retry，其余填入输入框）
    qq_clear_button: true         # QQ 单聊/群聊（频道除外）AI Markdown 回复附加"清空会话"指令按钮（type=2，仅填入输入框，不自动发送 /ai reset）
    qq_plan_button: true          # QQ 单聊/群聊（频道除外）Markdown 计划消息附加"查看计划/停止生成"指令按钮（/ai plan、/ai stop；stop 同时取消计划）
    fallback: false               # 无命令匹配时兜底回复（群聊+私聊全部非命令消息，group_autonomous+private_chat 并集）
    vision_enabled: false         # 是否支持图片附件（多模态）
    max_attachment_size: 0        # 附件大小上限
    skill_timeout: ""             # 技能执行超时
    skill_max_depth: 0            # 技能调用最大嵌套深度
```

## 工具调用

- **自动发现**：已注册的命令（如 `/ping`、`/pic`、`/sauce`、`/stats` 等）自动作为工具暴露给 LLM，无需额外配置
- **显式注册优先**：插件调用 `RegisterToolProvider` 显式注册的工具会**移除同名自动发现工具**及其命令映射，保证 LLM 调用插件自实现的 `Execute`
- **技能注册工具**：用户自定义技能（`u_` 前缀）也会作为工具可用

## 自定义技能（/ai skill）

技能是用户可注册的提示词模板，可被 AI 调用：

| 命令 | 说明 |
|------|------|
| `/ai skill add <名称> [内容]` | 注册技能（支持内联 Markdown 或 .md 附件；两步注册可只发名称后由系统等待内容） |
| `/ai skill list` | 列出我的技能 |
| `/ai skill remove <名称>` | 删除技能 |
| `/ai skill enable\|disable <名称>` | 启用/禁用技能 |
| `/ai skill promote <名称>` | 提升为系统级技能（所有用户可见可调用，需管理员） |
| `/ai skill info <名称>` | 查看技能详情（含 Prompt 预览） |

## 多模态（Vision）

启用 `vision_enabled` 后，对话中的图片附件会随消息发送给视觉模型（提供商需支持多模态）。

---

*完整的提供商模型兼容信息与 API 差异见 `builtin/ai/provider_*.go`。*
