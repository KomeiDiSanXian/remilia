package remilia

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestFileDeadLetterConsumer 测试文件死信消费者
func TestFileDeadLetterConsumer(t *testing.T) {
	// 创建临时文件
	tmpfile, err := os.CreateTemp("", "deadletter-*.jsonl")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	consumer := FileDeadLetterConsumer{Path: tmpfile.Name()}

	// 创建死信
	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-1",
		},
		Err:     errors.New("test error"),
		Attempt: 3,
		Source:  "test",
	}

	// 消费死信
	consumer.Consume(item)

	// 验证文件内容
	file, err := os.Open(tmpfile.Name())
	assert.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	assert.True(t, scanner.Scan(), "should have one line")

	line := scanner.Text()
	assert.NotEmpty(t, line)

	// 验证 JSON 格式
	var decoded map[string]interface{}
	err = json.Unmarshal([]byte(line), &decoded)
	assert.NoError(t, err)
	assert.Contains(t, decoded, "error")
}

// TestFileDeadLetterConsumerMultiple 测试多次写入
func TestFileDeadLetterConsumerMultiple(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "deadletter-*.jsonl")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	consumer := FileDeadLetterConsumer{Path: tmpfile.Name()}

	// 写入多个死信
	for i := 0; i < 3; i++ {
		item := DeadLetterItem{
			Event: &dto.Payload{
				Type: dto.C2CMessageCreate,
				ID:   dto.EventID("test-event-" + string(rune(i))),
			},
			Err:     errors.New("error " + string(rune(i))),
			Attempt: i,
			Source:  "test",
		}
		consumer.Consume(item)
	}

	// 验证文件有 3 行
	file, err := os.Open(tmpfile.Name())
	assert.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
	}
	assert.Equal(t, 3, lines)
}

// TestFileDeadLetterConsumerInvalidPath 测试无效路径
func TestFileDeadLetterConsumerInvalidPath(t *testing.T) {
	consumer := FileDeadLetterConsumer{Path: "/invalid/path/deadletter.jsonl"}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event",
		},
		Err:    errors.New("test error"),
		Source: "test",
	}

	// 应该记录错误但不 panic
	assert.NotPanics(t, func() {
		consumer.Consume(item)
	})
}

// TestWebhookDeadLetterConsumer 测试 Webhook 死信消费者
func TestWebhookDeadLetterConsumer(t *testing.T) {
	// 创建测试服务器
	received := false
	var receivedData map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true

		// 解析请求体
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&receivedData)
		assert.NoError(t, err)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := WebhookDeadLetterConsumer{
		URL:        server.URL,
		Timeout:    time.Second,
		MaxRetries: 2,
	}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-webhook",
		},
		Err:     errors.New("webhook test error"),
		Attempt: 1,
		Source:  "test",
	}

	// 消费死信
	consumer.Consume(item)

	// 验证服务器收到请求
	assert.True(t, received, "webhook should receive the request")
	assert.Contains(t, receivedData, "error")
}

// TestWebhookDeadLetterConsumerRetry 测试重试机制
func TestWebhookDeadLetterConsumerRetry(t *testing.T) {
	attempts := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// 前两次失败
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			// 第三次成功
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	consumer := WebhookDeadLetterConsumer{
		URL:        server.URL,
		Timeout:    time.Second,
		MaxRetries: 3,
	}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-retry",
		},
		Err:    errors.New("retry test"),
		Source: "test",
	}

	// 消费死信（应该重试）
	consumer.Consume(item)

	// 验证重试了 3 次
	assert.Equal(t, 3, attempts)
}

// TestWebhookDeadLetterConsumerTimeout 测试超时
func TestWebhookDeadLetterConsumerTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 延迟响应
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := WebhookDeadLetterConsumer{
		URL:        server.URL,
		Timeout:    50 * time.Millisecond,
		MaxRetries: 0,
	}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-timeout",
		},
		Err:    errors.New("timeout test"),
		Source: "test",
	}

	// 应该超时但不 panic
	assert.NotPanics(t, func() {
		consumer.Consume(item)
	})
}

// TestWebhookDeadLetterConsumerInvalidURL 测试无效 URL
func TestWebhookDeadLetterConsumerInvalidURL(t *testing.T) {
	consumer := WebhookDeadLetterConsumer{
		URL:        "http://invalid-url-that-does-not-exist:9999",
		Timeout:    100 * time.Millisecond,
		MaxRetries: 1,
	}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-invalid-url",
		},
		Err:    errors.New("invalid url test"),
		Source: "test",
	}

	// 应该记录错误但不 panic
	assert.NotPanics(t, func() {
		consumer.Consume(item)
	})
}

// TestKafkaDeadLetterConsumer 测试 Kafka 死信消费者（占位）
func TestKafkaDeadLetterConsumer(t *testing.T) {
	consumer := KafkaDeadLetterConsumer{
		Brokers: []string{"localhost:9092"},
		Topic:   "dead-letter",
	}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-kafka",
		},
		Err:    errors.New("kafka test"),
		Source: "test",
	}

	// 占位实现应该记录警告但不 panic
	assert.NotPanics(t, func() {
		consumer.Consume(item)
	})
}

// TestMarshalDeadLetterItem 测试序列化死信
func TestMarshalDeadLetterItem(t *testing.T) {
	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-marshal",
		},
		Err:     errors.New("test error"),
		Attempt: 3,
		Source:  "test",
	}

	data, err := MarshalDeadLetterItem(item)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	// 验证可以解析
	var decoded map[string]interface{}
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
}

// TestFileDeadLetterConsumerLargeData 测试大数据量
func TestFileDeadLetterConsumerLargeData(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "deadletter-large-*.jsonl")
	assert.NoError(t, err)
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	consumer := FileDeadLetterConsumer{Path: tmpfile.Name()}

	// 写入大量数据
	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "test-event-large",
		},
		Err:    errors.New(strings.Repeat("x", 10000)), // 用大错误消息测试
		Source: "test",
	}

	// 应该成功写入
	assert.NotPanics(t, func() {
		consumer.Consume(item)
	})

	// 验证文件存在且有内容
	info, err := os.Stat(tmpfile.Name())
	assert.NoError(t, err)
	assert.Greater(t, info.Size(), int64(100))
}

// BenchmarkFileDeadLetterConsumer 基准测试文件消费者
func BenchmarkFileDeadLetterConsumer(b *testing.B) {
	tmpfile, _ := os.CreateTemp("", "deadletter-bench-*.jsonl")
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	consumer := FileDeadLetterConsumer{Path: tmpfile.Name()}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "bench-event",
		},
		Err:    errors.New("benchmark test"),
		Source: "test",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		consumer.Consume(item)
	}
}

// BenchmarkWebhookDeadLetterConsumer 基准测试 Webhook 消费者
func BenchmarkWebhookDeadLetterConsumer(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := WebhookDeadLetterConsumer{
		URL:        server.URL,
		Timeout:    time.Second,
		MaxRetries: 0,
	}

	item := DeadLetterItem{
		Event: &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   "bench-event",
		},
		Err:    errors.New("benchmark test"),
		Source: "test",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		consumer.Consume(item)
	}
}
