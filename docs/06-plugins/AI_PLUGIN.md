# AI 对话插件指南

内置 `ai` 插件（`builtin/ai/`）提供多提供商（OpenAI / Anthropic）的 AI 对话、工具调用与自定义技能管理。

## 触发方式

- **@机器人** 后直接发消息（`at_bot: true`，默认）
- **私聊**（`private_chat: true`）
- 消息前缀触发：`trigger_cmd`（默认 `/ai`）

## 命令

| 命令 | 说明 |
|------|------|
| `/ai <消息>` | 与 AI 对话（支持工具调用与图片） |
| `/ai reset` | 清空当前会话对话历史 |
| `/ai undo` | 撤销上一条对话 |
| `/ai retry` | 重新生成上一条回复 |
| `/ai summary` | 后台生成对话总结 |
| `/ai status` | 查看会话状态（提供商/模型/消息数/时长） |
| `/ai stats` | 查看使用统计（LLM 调用次数、工具调用次数） |
| `/ai tools` | 列出当前可用工具 |
| `/ai skill ...` | 自定义技能管理（见下） |

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
    markdown: true                # 回复使用 Markdown
    fallback: false               # 非触发消息是否兜底回复
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
