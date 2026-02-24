package plugin

// v2.go — 原 v2 单文件已按职责拆分为以下子文件：
//
//   - descriptor.go  PluginDescriptor / PluginAdvanced / TeardownContext / 函数类型
//   - context.go     SetupContext / Must / Try / GetPlugin
//   - container.go   Container（依赖注入容器）
//   - instance.go    PluginInstance（运行时实例 + 公开 API）
//   - reload.go      reload / reloadBlueGreen（三种热重载策略）
//   - register.go    RegisterV2 / RegisterMultipleV2Atomic / Smart / 拓扑排序
