package remilia

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

// FileDeadLetterConsumer 将死信以 JSON 行写入文件
//
// 死信会被追加到指定文件，每个死信占一行 JSON。
// 如果文件写入失败，会记录错误日志但不会 panic。
type FileDeadLetterConsumer struct {
	Path string
}

// Consume 消费死信，写入文件
//
// 错误处理：
//   - 序列化失败：记录错误日志并返回
//   - 文件打开失败：记录错误日志并返回
//   - 写入失败：记录错误日志并返回
//   - 刷新失败：记录错误日志并返回
//
// 注意：所有错误都会被记录到日志，但不会阻断程序运行
func (f FileDeadLetterConsumer) Consume(item DeadLetterItem) {
	// 序列化死信
	b, err := MarshalDeadLetterItem(item)
	if err != nil {
		logrus.WithError(err).
			WithField("event_id", string(item.Event.ID)).
			WithField("event_type", item.Event.Type).
			Error("[DeadLetter] Failed to marshal dead letter item")
		return
	}

	// 打开文件
	file, err := os.OpenFile(f.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logrus.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to open dead letter file")
		return
	}
	defer file.Close()

	// 写入文件
	w := bufio.NewWriter(file)
	if _, err := w.Write(b); err != nil {
		logrus.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to write dead letter data")
		return
	}

	// 写入换行符
	if _, err := w.Write([]byte("\n")); err != nil {
		logrus.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to write newline")
		return
	}

	// 刷新缓冲区
	if err := w.Flush(); err != nil {
		logrus.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to flush buffer")
		return
	}

	// 成功记录
	logrus.WithFields(logrus.Fields{
		"event_id":   string(item.Event.ID),
		"event_type": item.Event.Type,
		"path":       f.Path,
		"attempt":    item.Attempt,
		"source":     item.Source,
	}).Debug("[DeadLetter] Dead letter item saved to file")
}

// WebhookDeadLetterConsumer 将死信以 POST 发送到指定 Webhook 地址
//
// 支持配置超时、重试次数等参数。
// 如果发送失败，会记录错误日志。
//
// MaxRetries 语义：
//   - MaxRetries < 0：使用默认重试次数（3）
//   - MaxRetries == 0：不重试（只尝试 1 次）
//   - MaxRetries  > 0：最多重试 MaxRetries 次（总尝试次数 = 1 + MaxRetries）
//
// 为了兼容旧行为（零值表示默认），推荐调用方显式设置 MaxRetries，或在配置加载阶段写入默认值。
type WebhookDeadLetterConsumer struct {
	URL        string        // Webhook 地址
	Timeout    time.Duration // 请求超时时间（默认 5 秒）
	MaxRetries int           // 最大重试次数（<0 表示使用默认 3；0 表示不重试）
}

// Consume 消费死信，发送到 Webhook
//
// 重试策略：
//   - 指数退避：1s, 2s, 4s ...
func (w WebhookDeadLetterConsumer) Consume(item DeadLetterItem) {
	// 设置默认值
	timeout := w.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	maxRetries := w.MaxRetries
	// Back-compat default: allow callers to opt into default by setting MaxRetries < 0.
	if maxRetries < 0 {
		maxRetries = 3
	}

	// 序列化死信
	b, err := MarshalDeadLetterItem(item)
	if err != nil {
		logrus.WithError(err).
			WithField("event_id", string(item.Event.ID)).
			WithField("webhook_url", w.URL).
			Error("[DeadLetter] Failed to marshal dead letter item for webhook")
		return
	}

	// 创建 HTTP 客户端
	client := &http.Client{
		Timeout: timeout,
	}

	// 重试发送：attempt=0 表示首次尝试；attempt>0 表示第 attempt 次重试
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
			logrus.WithFields(logrus.Fields{
				"attempt":     attempt + 1,
				"max_retries": maxRetries + 1,
				"backoff":     backoff,
			}).Debug("[DeadLetter] Retrying webhook request")
		}

		// 发送请求
		resp, err := client.Post(w.URL, "application/json", bytes.NewReader(b))
		if err != nil {
			lastErr = err
			logrus.WithError(err).
				WithField("attempt", attempt+1).
				WithField("webhook_url", w.URL).
				Warn("[DeadLetter] Webhook request failed")
			continue
		}

		// 检查响应状态
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// 成功
			resp.Body.Close()
			logrus.WithFields(logrus.Fields{
				"event_id":    string(item.Event.ID),
				"webhook_url": w.URL,
				"status_code": resp.StatusCode,
				"attempt":     attempt + 1,
			}).Debug("[DeadLetter] Dead letter sent to webhook successfully")
			return
		}

		// 非 2xx 响应
		resp.Body.Close()
		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		logrus.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"attempt":     attempt + 1,
			"webhook_url": w.URL,
		}).Warn("[DeadLetter] Webhook returned non-2xx status")
	}

	// 所有重试都失败
	logrus.WithError(lastErr).
		WithFields(logrus.Fields{
			"event_id":    string(item.Event.ID),
			"webhook_url": w.URL,
			"max_retries": maxRetries + 1,
		}).Error("[DeadLetter] Failed to send dead letter to webhook after all retries")
}

// KafkaDeadLetterConsumer Kafka 死信消费者（占位实现）
//
// 这是一个占位实现，实际项目中需要引入 Kafka 客户端库。
//
// 推荐的 Kafka 库：
//   - github.com/segmentio/kafka-go（纯 Go 实现，无 cgo 依赖）
//   - github.com/IBM/sarama（功能丰富，社区活跃）
//   - github.com/confluentinc/confluent-kafka-go（官方支持，依赖 librdkafka）
//
// 使用示例（使用 kafka-go）：
//
//	import "github.com/segmentio/kafka-go"
//
//	type KafkaDeadLetterConsumer struct {
//	    writer *kafka.Writer
//	}
//
//	func NewKafkaDeadLetterConsumer(brokers []string, topic string) *KafkaDeadLetterConsumer {
//	    return &KafkaDeadLetterConsumer{
//	        writer: &kafka.Writer{
//	            Addr:     kafka.TCP(brokers...),
//	            Topic:    topic,
//	            Balancer: &kafka.LeastBytes{},
//	        },
//	    }
//	}
//
//	func (k *KafkaDeadLetterConsumer) Consume(item DeadLetterItem) {
//	    b, err := MarshalDeadLetterItem(item)
//	    if err != nil {
//	        logrus.WithError(err).Error("[DeadLetter] Failed to marshal item for Kafka")
//	        return
//	    }
//
//	    err = k.writer.WriteMessages(context.Background(), kafka.Message{
//	        Key:   []byte(item.Event.ID),
//	        Value: b,
//	    })
//	    if err != nil {
//	        logrus.WithError(err).Error("[DeadLetter] Failed to write to Kafka")
//	        return
//	    }
//
//	    logrus.WithField("event_id", string(item.Event.ID)).
//	        Debug("[DeadLetter] Dead letter sent to Kafka")
//	}
//
//	func (k *KafkaDeadLetterConsumer) Close() error {
//	    return k.writer.Close()
//	}
type KafkaDeadLetterConsumer struct {
	Brokers []string
	Topic   string
}

// Consume 消费死信（占位实现）
//
// 注意：这是一个占位实现，仅记录警告日志。
// 实际项目中需要引入 Kafka 客户端库并实现真正的消息发送逻辑。
func (k KafkaDeadLetterConsumer) Consume(item DeadLetterItem) {
	logrus.WithFields(logrus.Fields{
		"event_id":   string(item.Event.ID),
		"event_type": item.Event.Type,
		"brokers":    k.Brokers,
		"topic":      k.Topic,
	}).Warn("[DeadLetter] KafkaDeadLetterConsumer is a placeholder implementation - dead letter not actually sent to Kafka")

	// TODO: 实现真正的 Kafka 发送逻辑
	// 参考上面的注释中的示例代码
}
