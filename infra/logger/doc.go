// Package logger 提供结构化日志功能。
//
// 该包封装了 zerolog，提供统一的日志接口：支持控制台、文件与自定义 Writer
// 多路输出，以及运行时调整日志级别与时间格式。
//
// # 主要功能
//
//   - 包级便捷函数：Trace / Debug / Info / Warn / Error / Fatal / Panic 及 f 版本
//   - 实例化 Logger：NewLogger(zerolog.Logger) 创建独立实例
//   - 链式字段：WithField / WithFields / WithError 返回 *LogWithFields
//   - 多路输出：Init 支持 console / file / extra writer 组合
//   - 测试辅助：InitNop / InitTest
//
// # 使用示例
//
//	// 初始化（进程入口）
//	if err := logger.Init(cfg.Log); err != nil {
//		log.Fatal(err)
//	}
//
//	// 包级函数
//	logger.Info("bot started")
//	logger.WithField("platform", "qq").Warn("reconnecting")
//	logger.WithError(err).Error("request failed")
//
//	// 独立实例
//	l := logger.NewLogger(zerolog.New(os.Stdout))
//	l.Info("component started")
package logger
