// Package ai 提供 AI 对话能力：多 LLM 提供商、工具调用、技能（Skill）、
// 长期记忆与计划推进。
//
// # 包结构
//
// 本包是 AI 插件的装配根。可脱离 Plugin 独立存在的部分按职责成子包，
// 依赖单向向下：
//
//		config  →  protocol  →  toolkit  →  retrieval
//		                            ↓
//		                         session
//		                            ↓
//		      catalog / decision / execution / promptctx
//		                            ↓
//		       runtime / textutil（叶子工具包）
//		                            ↓
//		                          ai
//
//	  - config：配置结构与加载
//	  - protocol：LLM 线格式（消息、工具声明、流事件）与提供商实现
//	  - toolkit：工具/动作/技能契约与注册表
//	  - retrieval：关键词与向量检索骨架（工具选择、记忆检索共用）
//	  - session：会话、计划、工具集稳定状态与持久化
//	  - catalog：内置动作目录与能力端口
//	  - decision：候选打分、Top-K 选择与工具集稳定策略
//	  - execution：动作执行、真实命令通道与审批闸门
//	  - promptctx：上下文供给（运行时、群窗口、长期记忆、相关历史）
//	  - runtime：单轮 LLM 调用、回答校验、事实抽取、消息与请求形状工具
//	  - textutil：跨 owner 复用的纯文本工具
//
// 本包保留装配与编排，并按"谁负责这项能力"把 Plugin 的字段分区到五个
// owner 结构体（见 pluginparts.go）：
//
//   - catalogState：工具/技能目录（发现、注册、权限过滤）
//   - contextState：上下文供给（历史、向量缓存、长期记忆）
//   - executionState：动作执行（真实命令通道、审批闸门）
//   - runtimeState：回合运行时（会话、触发命令、生命周期）
//   - adminState：管理与命令（子命令、群策略、用量、按钮、提醒、待办）
//
// # 核心流程
//
//  1. 入口 handleAI 分派三种触发路径，handleAIChat 执行对话回合（handler.go）
//  2. processWithTools 是主循环：调用 LLM → 执行工具 → 回填结果 → 下一轮（process.go）
//  3. decideTurnActions 决定本轮把哪些动作交给模型（decision.go）
//  4. executeToolResult 分派到函数动作、真实命令或 Skill 子代理循环（execute.go）
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
// 会话按 platform:chatID:userID 维度隔离，不同群组/用户互不干扰；
// LRU 缓存淘汰 + TTL 过期清理，可选 storage 插件持久化。见 builtin/ai/session。
//
// # 安全设计
//
// 自动发现工具时仅暴露不需要权限的命令（Permissions 为空），
// 防止通过 AI 绕过权限检查。需要权限的命令应通过 RegisterToolProvider 显式注册。
//
// 配置项、默认值与取值说明见 builtin/ai/config。
package ai
