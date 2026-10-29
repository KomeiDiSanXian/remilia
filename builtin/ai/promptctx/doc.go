// Package promptctx 是提示词上下文的供给侧：把"逐轮变化"的环境信息组装成一段
// 可注入的正文，交回装配侧挂到请求尾部。
//
// 各节职责：
//
//   - [BuildRuntimeContext]：当前事件的环境信息（时间、平台、会话、发送者）
//   - [BuildGroupWindowN]：同群最近若干条消息（含可选的机器人自身回复）
//   - [BuildMemoryContext]：与当前查询最相关的长期事实
//   - [BuildRAGContext]：相关历史消息（关键词 + 可选向量）
//   - [PrependReplyContext]：被回复消息与回复链
//
// 节序、预算编排与最终拼接由 [BuildWindowed] 统一负责，节内部不决定预算。
// 本包只读会话与消息历史，不写回、不含选择或执行策略。
package promptctx
