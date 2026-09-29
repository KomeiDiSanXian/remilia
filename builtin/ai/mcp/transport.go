package mcp

import "context"

// transport.go — 与单个服务器的消息通道抽象。
//
// 目前有两种实现：stdio（启动子进程）与 http（Streamable HTTP / SSE）。二者
// 的差异被封装在传输层内，上层的客户端只面对"收发 JSON-RPC 报文"。

// Transport 一条与服务器的消息通道。
type Transport interface {
	// Start 启动接收循环：每收到一条完整报文即调用 handler；handler 必须尽快
	// 返回，不得阻塞读取。Start 返回后通道即可 Send。
	Start(handler func([]byte)) error
	// Send 发送一条报文。请求-响应式传输（http）会在返回前把收到的响应交给
	// 已注册的 handler。
	Send(ctx context.Context, msg []byte) error
	// Close 关闭通道并释放资源（子进程、连接等）。
	Close() error
}
