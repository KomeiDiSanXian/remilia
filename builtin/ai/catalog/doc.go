// Package catalog 是内置动作（工具）目录与动作所需的能力端口。
//
// 每个动作文件只声明"动作是什么"（名称、描述、参数 Schema、策略）与"怎么执行"，
// 执行期需要的外部能力经 [Capabilities] 端口注入，因此本包不反向依赖插件实例
// 与会话存储。目录的聚合、来源仲裁、快照与代数由装配侧（builtin/ai）承担。
//
// 动作命名与来源见 toolkit（[toolkit.Action] / [toolkit.Tool]）；线格式序列化见
// builtin/ai/protocol。
package catalog
