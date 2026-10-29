// Package session 是 AI 插件被各层共享的唯一可变运行时状态：消息历史、回合标志、
// 计划推进预算、附件缓存，以及选路/检索/工具集三类会话级缓存。
//
// 本包只回答三件事：
//
//   - 会话是什么（[Session] 的字段与持久化边界）
//   - 如何安全读写（[Session.Lock] / [Session.LockTurn] / 原子回合信号）
//   - 如何被管理（[SessionManager] 的 LRU、TTL、消息裁剪与可选持久化）
//
// 不含选择、检索与执行策略：这些属于 builtin/ai/decision、builtin/ai/promptctx
// 与 builtin/ai 的装配层。缓存槽位（[SelectionCache] / [RAGCache] / [ToolSetState]）
// 由使用方定义复用策略，本包只保存数据。
package session
