// Package logger 提供结构化日志功能。
//
// 该包封装了 logrus，提供统一的日志接口和字段管理，便于日志查询和分析。
//
// 主要功能：
//   - 统一的日志字段名称常量
//   - 结构化日志记录器（StructuredLogger）
//   - 便捷的上下文字段提取（WithContext, WithMatcher, WithPlugin）
//   - 全局日志实例（按组件分类）
//
// 使用示例：
//
//	logger := logger.NewLogger("my-component")
//	logger.Info("component started")
//
//	// 使用预定义的全局日志器
//	logger.GetEngineLogger().WithContext(ctx).Info("processing event")
package logger
