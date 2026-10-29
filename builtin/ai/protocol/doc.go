// Package protocol 是 LLM 的线格式层：消息与多模态片段、工具声明与调用解析、
// 流式事件、重试循环，以及 OpenAI 兼容 / Anthropic 两个 Provider 实现。
//
// 本包刻意不依赖可观测性、不依赖插件配置：提供商实例的选择与指标包装在装配侧
// （builtin/ai 的 newProvider），调用用途与会话范围的标注通过 context 值传递
// （见 [WithCallKind] / [WithCallScope]），Provider 实现不读取它们。
//
// 错误类型 [APIError] 保留原始状态码与响应体，供调用方分类（限流/鉴权/超时/上下文超长）。
package protocol
