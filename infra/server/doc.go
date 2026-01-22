// Package server 提供HTTP服务器封装和生命周期管理。
//
// 该包提供了便捷的HTTP服务器启动和关闭管理，支持优雅停止。
//
// 主要功能：
//   - HTTPServer: HTTP服务器封装
//   - 自动后台启动
//   - 优雅关闭支持
//   - 并发安全的生命周期管理
//
// 使用示例：
//
//	server := server.NewHTTPServer("localhost:8080", myHandler)
//	server.Start()
//
//	// ... 应用运行 ...
//
//	// 优雅关闭
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	if err := server.Shutdown(ctx); err != nil {
//	    log.Printf("Shutdown error: %v", err)
//	}
package server
