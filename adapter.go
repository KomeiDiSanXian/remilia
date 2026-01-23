package remilia

import (
	"context"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/sirupsen/logrus"
)

// Adapter connects an event source to the Bot
// It is responsible for receiving events from external sources and delivering them to the bot
type Adapter interface {
	// Start starts the adapter and begins processing events
	// The handleFunc will be called for each received event
	Start(ctx context.Context, handleFunc func(*dto.Payload)) error

	// Stop gracefully shuts down the adapter
	Stop(ctx context.Context) error
}

// Webhook 是 webhook 的最小接口，只需要 EventStream
type Webhook interface {
	EventStream() <-chan *dto.Payload
}

// webhookAdapter 将 Webhook 适配为 core.Adapter
type webhookAdapter struct {
	webhook Webhook
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewWebhookAdapter 创建一个 webhook adapter
func NewWebhookAdapter(wh Webhook) Adapter {
	return &webhookAdapter{
		webhook: wh,
	}
}

// Start 启动 adapter
func (a *webhookAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	// 验证 EventStream 是否为 nil
	eventCh := a.webhook.EventStream()
	if eventCh == nil {
		return fmt.Errorf("EventStream returned nil channel")
	}

	a.ctx, a.cancel = context.WithCancel(ctx)

	// 启动事件循环
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				logrus.Debug("[Adapter] Context done, stopping event loop")
				return
			case event, ok := <-eventCh:
				if !ok {
					logrus.Warn("[Adapter] EventStream closed, stopping event loop")
					return
				}
				if event != nil {
					// 使用 defer+recover 包装 handler 调用，防止 panic 导致 goroutine 退出
					safeHandle(handler, event)
				}
			}
		}
	}()

	return nil
}

func safeHandle(handler func(*dto.Payload), event *dto.Payload) {
	defer func() {
		if r := recover(); r != nil {
			logrus.WithField("panic", r).Error("[Adapter] Handler panic recovered")
		}
	}()
	handler(event)
}

// Shutdown 关闭 adapter
func (a *webhookAdapter) Stop(context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}
