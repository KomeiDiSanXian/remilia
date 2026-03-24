package dlq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
)

// 预定义错误
var (
	ErrQueueClosed       = errors.New("queue is closed")
	ErrQueueFull         = errors.New("queue is full")
	ErrInvalidDropPolicy = errors.New("invalid drop policy")
	ErrCloseTimeout      = errors.New("close operation timed out")
)

// DropPolicy 定义队列满时的丢弃策略。
type DropPolicy int

const (
	DropPolicyOldest          DropPolicy = iota // 队列满时丢弃最旧的条目
	DropPolicyNewest                            // 队列满时丢弃最新的条目
	DropPolicyBlockUntilSpace                   // 阻塞直到有可用空间
)

// Stats 保存当前队列统计信息。
type Stats struct {
	QueueSize  int
	MaxSize    int
	Processed  int64
	Dropped    int64
	Workers    int
	IsClosed   bool
	Consumers  int
	DropPolicy DropPolicy
}

// Item 表示一个泛型死信条目。
//
// 这是 DeadLetterItem 的类型安全版本，可与任意数据类型配合使用。
//
// 示例：
//
//	// 用于 HTTP 请求
//	type FailedRequest struct {
//	    URL     string
//	    Body    []byte
//	    Headers map[string]string
//	}
//	httpItem := dlq.Item[*FailedRequest]{
//	    Data:    &FailedRequest{URL: "https://api.example.com"},
//	    Err:     errors.New("connection timeout"),
//	    Attempt: 3,
//	    Source:  "http-client",
//	}
type Item[T any] struct {
	Data    T      // 失败的数据
	Err     error  // 导致失败的错误
	Attempt int    // 重试次数
	Source  string // 来源标识符
}

// Consumer 消费类型为 T 的死信条目。
//
// 实现应线程安全，因为可能被多个 worker 并发调用。
type Consumer[T any] interface {
	Consume(item Item[T])
}

// Config 配置泛型死信队列。
type Config[T any] struct {
	MaxSize     int                                        // 最大队列容量
	Workers     int                                        // 消费者 worker 数量
	DropPolicy  DropPolicy                                 // 队列满时的丢弃策略
	OnDropped   func(item Item[T], reason string)          // 条目被丢弃时的回调
	OnProcessed func(item Item[T], duration time.Duration) // 条目被处理时的回调
}

// Queue 是可处理任意数据类型的泛型死信队列。
//
// 功能与 DeadLetterQueue 相同，但具有类型安全性。
//
// 示例：
//
//	// 为失败的 HTTP 请求创建 DLQ
//	type FailedRequest struct {
//	    URL  string
//	    Body []byte
//	}
//
//	dlq := dlq.New[*FailedRequest](dlq.Config[*FailedRequest]{
//	    MaxSize: 1000,
//	    Workers: 4,
//	    OnDropped: func(item dlq.Item[*FailedRequest], reason string) {
//	        log.Printf("Dropped request to %s: %s", item.Data.URL, reason)
//	    },
//	})
//
//	// Add a consumer
//	dlq.AddConsumer(myHTTPConsumer)
//
//	// Start processing
//	dlq.Start()
//
//	// Enqueue failed requests
//	dlq.Enqueue(dlq.Item[*FailedRequest]{
//	    Data:    &FailedRequest{URL: "https://api.example.com"},
//	    Err:     errors.New("timeout"),
//	    Attempt: 3,
//	})
type Queue[T any] struct {
	config       Config[T]
	queue        chan Item[T]
	consumers    []Consumer[T]
	consumerSnap *infraatomic.Value[[]Consumer[T]]

	dropped   atomic.Int64
	processed atomic.Int64

	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex
	enqueueMu   sync.Mutex
	queueClosed atomic.Bool
	closeOnce   sync.Once
}
