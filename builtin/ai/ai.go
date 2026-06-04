// Package ai 提供 AI 对话能力，支持多 LLM 提供商、工具调用与技能（Skill）系统。
//
// # 架构概览
//
// 本包是 Remilia Bot 的 AI 插件核心，围绕三个层次构建：
//
//  1. 消息层（handler.go, subcommand.go）
//     处理用户消息的路由与分发。
//     支持命令触发（/ai）、@机器人、私聊三种入口。
//
//  2. 编排层（process.go, execute.go, discovery.go）
//     processWithTools 是主循环：调用 LLM → 解析工具请求 → 执行工具 → 回填结果 → 下一轮。
//     executeTool 分派到具体工具或 Skill，executeSkill 启动子代理循环。
//     discovery.go 负责自动发现框架中的安全命令并包装为 LLM 工具。
//
//  3. 提供商层（provider.go, provider_openai.go, provider_anthropic.go）
//     Provider 接口抽象了不同 LLM API 的差异。
//     当前内置 OpenAI 兼容 API 和 Anthropic Messages API 两种实现。
//
// # 核心数据结构
//
//   - Plugin: 插件主结构体，持有配置、会话管理器、工具注册表、技能注册表等
//   - Config: 配置项，通过 config.yaml 的 plugins.ai 节读取
//   - Session / SessionManager: 会话管理，LRU 缓存 + 可选 GORM 持久化
//   - Tool / ToolRegistry: LLM 可调用的工具注册表
//   - Skill / SkillRegistry: 子代理（Skill）注册表，每个 Skill 拥有独立的 Prompt 和工具集
//   - Provider: LLM 提供商接口，支持 Chat（非流式）和 ChatStream（流式）
//
// # 安全设计
//
// 自动发现工具时仅暴露不需要权限的命令（Permissions 为空），
// 防止通过 AI 绕过权限检查。需要权限的命令应通过 RegisterToolProvider 显式注册。
//
// # 触发方式
//
// 三种触发方式可组合使用：
//   - /ai <消息> 命令
//   - @机器人 <消息>（群聊中 @机器人 触发）
//   - 私聊自动响应（过滤以 "/" 开头的命令消息）
//
// # 会话管理
//
// 会话按 platform:chatID:userID 维度隔离，不同群组/用户互不干扰。
// 支持 LRU 缓存淘汰（默认最大 1000 个会话）和 TTL 过期清理（默认 24 小时）。
// 可选 storage 插件实现 GORM 持久化。
//
// # 工具调用流程
//
//  1. processWithTools 发送消息 + 工具列表到 LLM（流式）
//  2. LLM 返回文本和/或工具调用请求
//  3. executeTool 分派：先检查是否为 Skill，再尝试真实命令，最后回退到占位 Execute 回调
//  4. 工具结果以 tool 角色消息回填对话，进入下一轮
//  5. 达到 MaxDepth（默认 5）或无工具调用时停止
//
// # 技能（Skill）系统
//
// Skill 是一个可嵌套的子代理：拥有独立的 System Prompt 和工具集。
//   - Skill 自动注册为 Tool 供 LLM 调用
//   - 每个 Skill 内部工具列表 = 自有工具 + 其他 Skill（作为可调用工具注入）
//   - Skill 使用非流式 LLM 调用（runSingleRound），支持 SkillMaxDepth 轮工具循环
//   - 其他插件通过实现 SkillProvider 接口注册 Skill（自动发现模式）
//
// # 配置参考
//
// plugins:
//
//	ai:
//	  provider: "openai"              # openai / anthropic
//	  model: "gpt-4o-mini"            # 模型名称
//	  base_url: "https://api.openai.com/v1"
//	  api_key: "${AI_API_KEY}"        # 环境变量引用
//	  system_prompt: "你是一个有用的AI助手"
//	  at_bot: true
//	  private_chat: true
//	  markdown: true
//	  max_depth: 5                    # 最大工具调用轮数
//	  skill_max_depth: 3              # Skill 内最大调用深度
//	  api_timeout: 60s
//	  tool_timeout: 30s
//	  skill_timeout: 60s
//	  session_ttl: 24h
package ai
