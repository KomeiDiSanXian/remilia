package dlq

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileConsumer_Consume 测试文件消费者
func TestFileConsumer_Consume(t *testing.T) {
	t.Run("successful write", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "dead_letters.log")

		consumer := FileConsumer{Path: filePath}

		// 创建测试项目
		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "test-event-1",
				Type: "test.event",
			},
			Err:     nil,
			Attempt: 3,
			Source:  "test-handler",
		}

		// 消费
		consumer.Consume(item)

		// 验证文件内容
		content, err := os.ReadFile(filePath)
		require.NoError(t, err)

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		assert.Len(t, lines, 1)

		// 验证 JSON 格式
		var result map[string]any
		err = json.Unmarshal([]byte(lines[0]), &result)
		require.NoError(t, err)

		event := result["event"].(map[string]any)
		assert.Equal(t, "test-event-1", event["id"])
		assert.Equal(t, "test.event", event["type"])

		errorInfo := result["error"].(map[string]any)
		assert.Equal(t, "test-handler", errorInfo["source"])
		assert.Equal(t, float64(3), errorInfo["attempt"])
	})

	t.Run("append multiple items", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "dead_letters.log")

		consumer := FileConsumer{Path: filePath}

		// 写入多个项目
		for i := 1; i <= 3; i++ {
			item := DeadLetterItem{
				Event: &dto.Payload{
					ID:   dto.EventID("event-" + string(rune('0'+i))),
					Type: "test",
				},
				Attempt: i,
			}
			consumer.Consume(item)
		}

		// 验证文件有 3 行
		content, err := os.ReadFile(filePath)
		require.NoError(t, err)

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		assert.Len(t, lines, 3)
	})

	t.Run("with error message", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "dead_letters.log")

		consumer := FileConsumer{Path: filePath}

		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "error-event",
				Type: "test",
			},
			Err:     assert.AnError,
			Attempt: 1,
			Source:  "test",
		}

		consumer.Consume(item)

		// 验证错误信息被记录
		content, err := os.ReadFile(filePath)
		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(content, &result)
		require.NoError(t, err)

		errorInfo := result["error"].(map[string]any)
		assert.NotEmpty(t, errorInfo["message"])
	})

	t.Run("create directory if not exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "subdir", "nested", "dead_letters.log")

		// 创建目录
		err := os.MkdirAll(filepath.Dir(filePath), 0755)
		require.NoError(t, err)

		consumer := FileConsumer{Path: filePath}

		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "nested-event",
				Type: "test",
			},
		}

		consumer.Consume(item)

		// 验证文件被创建
		_, err = os.Stat(filePath)
		assert.NoError(t, err)
	})
}

// TestWebhookConsumer_Consume 测试 Webhook 消费者
func TestWebhookConsumer_Consume(t *testing.T) {
	t.Run("successful request", func(t *testing.T) {
		var requestCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			// 读取并验证 body
			var body map[string]any
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.NotNil(t, body["event"])

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		consumer := WebhookConsumer{
			URL:        server.URL,
			Timeout:    2 * time.Second,
			MaxRetries: 0,
		}

		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "webhook-event",
				Type: "test",
			},
			Attempt: 1,
		}

		consumer.Consume(item)

		// 验证请求被发送
		assert.Equal(t, int32(1), requestCount.Load())
	})

	t.Run("retry on failure", func(t *testing.T) {
		var requestCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := requestCount.Add(1)
			if count < 3 {
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		consumer := WebhookConsumer{
			URL:        server.URL,
			Timeout:    2 * time.Second,
			MaxRetries: 3,
		}

		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "retry-event",
				Type: "test",
			},
		}

		consumer.Consume(item)

		// 验证重试次数
		assert.GreaterOrEqual(t, requestCount.Load(), int32(3))
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		var requestCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		consumer := WebhookConsumer{
			URL:        server.URL,
			Timeout:    1 * time.Second,
			MaxRetries: 2,
		}

		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "max-retry-event",
				Type: "test",
			},
		}

		consumer.Consume(item)

		// 验证最大重试次数（1 次初始 + 2 次重试 = 3 次）
		assert.Equal(t, int32(3), requestCount.Load())
	})

	t.Run("default timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		consumer := WebhookConsumer{
			URL:        server.URL,
			MaxRetries: 0,
			// Timeout 未设置，应使用默认 5s
		}

		item := DeadLetterItem{
			Event: &dto.Payload{ID: "default-timeout-event"},
		}

		// 不应该超时
		consumer.Consume(item)
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		consumer := WebhookConsumer{
			URL:        server.URL,
			Timeout:    100 * time.Millisecond,
			MaxRetries: 0,
		}

		item := DeadLetterItem{
			Event: &dto.Payload{ID: "timeout-event"},
		}

		// 应该超时
		consumer.Consume(item)
	})

	t.Run("non-2xx status code", func(t *testing.T) {
		testCases := []int{
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusInternalServerError,
		}

		for _, statusCode := range testCases {
			t.Run(http.StatusText(statusCode), func(t *testing.T) {
				var requestCount atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestCount.Add(1)
					w.WriteHeader(statusCode)
				}))
				defer server.Close()

				consumer := WebhookConsumer{
					URL:        server.URL,
					Timeout:    1 * time.Second,
					MaxRetries: 1,
				}

				item := DeadLetterItem{
					Event: &dto.Payload{ID: "status-test"},
				}

				consumer.Consume(item)

				// 应该重试
				assert.Equal(t, int32(2), requestCount.Load())
			})
		}
	})

	t.Run("default max retries", func(t *testing.T) {
		var requestCount atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		consumer := WebhookConsumer{
			URL:        server.URL,
			MaxRetries: -1, // 使用默认值
		}

		item := DeadLetterItem{
			Event: &dto.Payload{ID: "default-retry"},
		}

		consumer.Consume(item)

		// 默认应该是 3 次重试（总共 4 次请求）
		assert.Equal(t, int32(4), requestCount.Load())
	})
}

// TestKafkaConsumer_Consume 测试 Kafka 消费者
func TestKafkaConsumer_Consume(t *testing.T) {
	t.Run("placeholder implementation", func(t *testing.T) {
		consumer := KafkaConsumer{
			Brokers: []string{"localhost:9092"},
			Topic:   "dead-letters",
		}

		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "kafka-event",
				Type: "test",
			},
		}

		// 不应该 panic
		consumer.Consume(item)
	})
}

// TestMarshalDeadLetterItem 测试序列化
func TestMarshalDeadLetterItem(t *testing.T) {
	t.Run("complete item", func(t *testing.T) {
		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "marshal-test",
				Type: "test.event",
			},
			Err:     assert.AnError,
			Attempt: 5,
			Source:  "test-source",
		}

		data, err := MarshalDeadLetterItem(item)
		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)

		event := result["event"].(map[string]any)
		assert.Equal(t, "marshal-test", event["id"])
		assert.Equal(t, "test.event", event["type"])

		errorInfo := result["error"].(map[string]any)
		assert.NotEmpty(t, errorInfo["message"])
		assert.Equal(t, "test-source", errorInfo["source"])
		assert.Equal(t, float64(5), errorInfo["attempt"])
	})

	t.Run("nil error", func(t *testing.T) {
		item := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "no-error",
				Type: "test",
			},
			Err: nil,
		}

		data, err := MarshalDeadLetterItem(item)
		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)

		errorInfo := result["error"].(map[string]any)
		assert.Empty(t, errorInfo["message"])
	})

	t.Run("minimal item", func(t *testing.T) {
		item := DeadLetterItem{
			Event: &dto.Payload{
				ID: "minimal",
			},
		}

		data, err := MarshalDeadLetterItem(item)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	})
}

// TestFileConsumer_ReadBack 测试文件写入和读取
func TestFileConsumer_ReadBack(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "read_back.log")

	consumer := FileConsumer{Path: filePath}

	// 写入多个项目
	items := []DeadLetterItem{
		{
			Event:   &dto.Payload{ID: "item-1", Type: "type1"},
			Attempt: 1,
			Source:  "source1",
		},
		{
			Event:   &dto.Payload{ID: "item-2", Type: "type2"},
			Attempt: 2,
			Source:  "source2",
		},
		{
			Event:   &dto.Payload{ID: "item-3", Type: "type3"},
			Attempt: 3,
			Source:  "source3",
		},
	}

	for _, item := range items {
		consumer.Consume(item)
	}

	// 读取并验证
	file, err := os.Open(filePath)
	require.NoError(t, err)
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var result map[string]any
		err := json.Unmarshal(scanner.Bytes(), &result)
		require.NoError(t, err)
		assert.NotNil(t, result["event"])
		assert.NotNil(t, result["error"])
	}

	require.NoError(t, scanner.Err())
	assert.Equal(t, 3, lineCount)
}

// BenchmarkFileConsumer 基准测试文件消费者
func BenchmarkFileConsumer(b *testing.B) {
	tmpDir := b.TempDir()
	filePath := filepath.Join(tmpDir, "benchmark.log")

	consumer := FileConsumer{Path: filePath}
	item := DeadLetterItem{
		Event: &dto.Payload{
			ID:   "bench-event",
			Type: "benchmark",
		},
		Attempt: 1,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		consumer.Consume(item)
	}
}

// BenchmarkWebhookConsumer 基准测试 Webhook 消费者
func BenchmarkWebhookConsumer(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	consumer := WebhookConsumer{
		URL:        server.URL,
		Timeout:    2 * time.Second,
		MaxRetries: 0,
	}

	item := DeadLetterItem{
		Event: &dto.Payload{
			ID:   "bench-event",
			Type: "benchmark",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		consumer.Consume(item)
	}
}

// BenchmarkMarshalDeadLetterItem 基准测试序列化
func BenchmarkMarshalDeadLetterItem(b *testing.B) {
	item := DeadLetterItem{
		Event: &dto.Payload{
			ID:   "bench-marshal",
			Type: "test",
		},
		Err:     assert.AnError,
		Attempt: 3,
		Source:  "benchmark",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = MarshalDeadLetterItem(item)
	}
}
