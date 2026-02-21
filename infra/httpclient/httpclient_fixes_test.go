package httpclient

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetJSON_MarshalError 验证修复 #9：SetJSON marshal 失败时 Do() 返回错误
func TestSetJSON_MarshalError(t *testing.T) {
	client := NewClient()

	// chan 类型无法 JSON 序列化，会触发 marshal 错误
	invalidData := make(chan int)

	req := client.Post("/test").SetJSON(invalidData)
	require.NotNil(t, req)
	assert.NotNil(t, req.buildErr, "buildErr should be set on marshal failure")

	_, err := req.Do()
	assert.Error(t, err, "Do() should return error when SetJSON failed")
	assert.Contains(t, err.Error(), "SetJSON", "Error should mention SetJSON")
}

// TestSetJSON_ValidData 验证 SetJSON 正常数据仍然工作
func TestSetJSON_ValidData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient().SetBaseURL(server.URL)
	resp, err := client.Post("/").SetJSON(map[string]string{"key": "value"}).Do()
	require.NoError(t, err)
	defer resp.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestRequest_Middlewares_NoConcurrentRace 验证修复 #10：并发 Request 不共享 middlewares 底层数组
func TestRequest_Middlewares_NoConcurrentRace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 创建带有 middleware 的 Client
	client := NewClient().
		SetBaseURL(server.URL).
		Use(func(r *Request) error { return nil })

	const concurrency = 20
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	for range concurrency {
		wg.Go(func() {
			// 每个 Request 添加自己的 middleware（测试切片不共享）
			req := client.Get("/").Use(func(r *Request) error { return nil })
			resp, err := req.Do()
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Close()
		})
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrent request failed: %v", err)
	}
}

// TestRequest_BuildErr_Cleared 验证新创建的 Request 没有残留 buildErr
func TestRequest_BuildErr_Cleared(t *testing.T) {
	client := NewClient()

	req := client.Get("/test")
	assert.Nil(t, req.buildErr, "Fresh request should have no buildErr")

	// 正常的 SetJSON 也不应产生 buildErr
	req2 := client.Post("/test").SetJSON(map[string]string{"a": "b"})
	assert.Nil(t, req2.buildErr, "Valid SetJSON should not set buildErr")
}
