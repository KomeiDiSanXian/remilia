package dlq

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// FileConsumer writes dead letters as JSON lines to a file.
//
// Dead letters are appended to the specified file, one JSON object per line.
// If writing fails, errors are logged but the application won't panic.
type FileConsumer struct {
	Path string
}

// Consume consumes a dead letter item and writes it to the file.
//
// Error handling:
//   - Serialization failure: logs error and returns
//   - File open failure: logs error and returns
//   - Write failure: logs error and returns
//   - Flush failure: logs error and returns
//
// Note: All errors are logged but won't block program execution
func (f FileConsumer) Consume(item DeadLetterItem) {
	// Serialize dead letter
	b, err := MarshalDeadLetterItem(item)
	if err != nil {
		logger.WithError(err).
			WithField("event_id", string(item.Event.ID)).
			WithField("event_type", item.Event.Type).
			Error("[DeadLetter] Failed to marshal dead letter item")
		return
	}

	// Open file
	file, err := os.OpenFile(f.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logger.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to open dead letter file")
		return
	}
	defer file.Close()

	// Write to file
	w := bufio.NewWriter(file)
	if _, err := w.Write(b); err != nil {
		logger.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to write dead letter data")
		return
	}

	// Write newline
	if _, err := w.Write([]byte("\n")); err != nil {
		logger.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to write newline")
		return
	}

	// Flush buffer
	if err := w.Flush(); err != nil {
		logger.WithError(err).
			WithField("path", f.Path).
			WithField("event_id", string(item.Event.ID)).
			Error("[DeadLetter] Failed to flush buffer")
		return
	}

	// Success log
	logger.WithFields(logger.Fields{
		"event_id":   string(item.Event.ID),
		"event_type": item.Event.Type,
		"path":       f.Path,
		"attempt":    item.Attempt,
		"source":     item.Source,
	}).Debug("[DeadLetter] Dead letter item saved to file")
}

// WebhookConsumer sends dead letters to a webhook via HTTP POST.
//
// Supports configurable timeout, retry count, etc.
// If sending fails, errors are logged.
//
// MaxRetries semantics:
//   - MaxRetries < 0: use default retry count (3)
//   - MaxRetries == 0: no retry (only 1 attempt)
//   - MaxRetries  > 0: retry up to MaxRetries times (total attempts = 1 + MaxRetries)
//
// For backward compatibility (zero value = default), it's recommended that callers
// explicitly set MaxRetries, or set defaults during config loading.
type WebhookConsumer struct {
	URL        string        // Webhook URL
	Timeout    time.Duration // Request timeout (default 5s)
	MaxRetries int           // Max retries (<0 = use default 3; 0 = no retry)
}

// Consume consumes a dead letter item and sends it to the webhook.
//
// Retry strategy:
//   - Exponential backoff: 1s, 2s, 4s ...
func (w WebhookConsumer) Consume(item DeadLetterItem) {
	// Set defaults
	timeout := w.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	maxRetries := w.MaxRetries
	// Back-compat default: allow callers to opt into default by setting MaxRetries < 0.
	if maxRetries < 0 {
		maxRetries = 3
	}

	// Serialize dead letter
	b, err := MarshalDeadLetterItem(item)
	if err != nil {
		logger.WithError(err).
			WithField("event_id", string(item.Event.ID)).
			WithField("webhook_url", w.URL).
			Error("[DeadLetter] Failed to marshal dead letter item for webhook")
		return
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: timeout,
	}

	// Retry sending: attempt=0 means first attempt; attempt>0 means retry attempt
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			time.Sleep(backoff)
			logger.WithFields(logger.Fields{
				"attempt":     attempt + 1,
				"max_retries": maxRetries + 1,
				"backoff":     backoff,
			}).Debug("[DeadLetter] Retrying webhook request")
		}

		// Send request
		resp, err := client.Post(w.URL, "application/json", bytes.NewReader(b))
		if err != nil {
			lastErr = err
			logger.WithError(err).
				WithField("attempt", attempt+1).
				WithField("webhook_url", w.URL).
				Warn("[DeadLetter] Webhook request failed")
			continue
		}

		// Check response status
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Success
			resp.Body.Close()
			logger.WithFields(logger.Fields{
				"event_id":    string(item.Event.ID),
				"webhook_url": w.URL,
				"status_code": resp.StatusCode,
				"attempt":     attempt + 1,
			}).Debug("[DeadLetter] Dead letter sent to webhook successfully")
			return
		}

		// Non-2xx response
		resp.Body.Close()
		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		logger.WithFields(logger.Fields{
			"status_code": resp.StatusCode,
			"attempt":     attempt + 1,
			"webhook_url": w.URL,
		}).Warn("[DeadLetter] Webhook returned non-2xx status")
	}

	// All retries failed
	logger.WithError(lastErr).
		WithFields(logger.Fields{
			"event_id":    string(item.Event.ID),
			"webhook_url": w.URL,
			"max_retries": maxRetries + 1,
		}).Error("[DeadLetter] Failed to send dead letter to webhook after all retries")
}

// KafkaConsumer is a Kafka dead letter consumer (placeholder implementation).
//
// This is a placeholder implementation. Actual projects need to import a Kafka client library.
//
// Recommended Kafka libraries:
//   - github.com/segmentio/kafka-go (pure Go, no cgo)
//   - github.com/IBM/sarama (feature-rich, active community)
//   - github.com/confluentinc/confluent-kafka-go (official support, depends on librdkafka)
//
// Example usage (using kafka-go):
//
//	import "github.com/segmentio/kafka-go"
//
//	type KafkaConsumer struct {
//	    writer *kafka.Writer
//	}
//
//	func NewKafkaConsumer(brokers []string, topic string) *KafkaConsumer {
//	    return &KafkaConsumer{
//	        writer: &kafka.Writer{
//	            Addr:     kafka.TCP(brokers...),
//	            Topic:    topic,
//	            Balancer: &kafka.LeastBytes{},
//	        },
//	    }
//	}
//
//	func (k *KafkaConsumer) Consume(item DeadLetterItem) {
//	    b, err := MarshalDeadLetterItem(item)
//	    if err != nil {
//	        logger.WithError(err).Error("[DeadLetter] Failed to marshal item for Kafka")
//	        return
//	    }
//
//	    err = k.writer.WriteMessages(context.Background(), kafka.Message{
//	        Key:   []byte(item.Event.ID),
//	        Value: b,
//	    })
//	    if err != nil {
//	        logger.WithError(err).Error("[DeadLetter] Failed to write to Kafka")
//	        return
//	    }
//
//	    logger.WithField("event_id", string(item.Event.ID)).
//	        Debug("[DeadLetter] Dead letter sent to Kafka")
//	}
//
//	func (k *KafkaConsumer) Close() error {
//	    return k.writer.Close()
//	}
//
// KafkaConsumer 是 Kafka 死信消费者的预留接口实现。
//
// Deprecated: 此实现尚未完成，调用 Consume 时不会真正发送消息到 Kafka，
// 仅打印错误日志。若在生产环境中配置 dead_letter.target: kafka，
// 死信消息将会丢失。请勿在生产环境中使用，直至 Kafka 集成完成。
//
// 需要集成 Kafka 的项目请参考结构体上方的注释示例代码，引入
// github.com/segmentio/kafka-go 或类似库自行实现 Consumer 接口。
type KafkaConsumer struct {
	Brokers []string
	Topic   string
}

// Consume 是 KafkaConsumer 的占位实现，不会真正发送消息到 Kafka。
//
// 此方法仅打印 ERROR 级别日志以提示配置错误。
// 若需要真实的 Kafka 投递，请参考上方注释中的示例代码自行实现 [Consumer] 接口。
func (k KafkaConsumer) Consume(item DeadLetterItem) {
	logger.WithFields(logger.Fields{
		"event_id":   string(item.Event.ID),
		"event_type": item.Event.Type,
		"brokers":    k.Brokers,
		"topic":      k.Topic,
	}).Error("[DeadLetter] KafkaConsumer is NOT implemented — dead letter message dropped. " +
		"Do NOT use KafkaConsumer in production until Kafka integration is complete.")
}
