// Package toolkit 定义工具/动作/技能的契约、注册表与调用期上下文。
//
// 三条分界线：
//
//   - 动作是什么（[ActionSpec]）与它是否该留下（[ActionPolicy]）分开，
//     合成管线货币 [Action]；执行载荷只在注册表里按名称取用（[ToolRegistry.Get]）。
//   - 调用期能力（调用者身份、授权器、发送/解析能力、审计来源、领域端口）
//     经 [Invocation] 显式传入，不放进 std context；context 只承载取消/超时与追踪。
//   - 富结果 [ToolResult] 不再把附件与结构化数据压成字符串。
//
// 外部插件经 [ToolProvider] / [SkillProvider] 接入；线格式（参数 Schema 与工具声明）
// 见 builtin/ai/protocol。
package toolkit
