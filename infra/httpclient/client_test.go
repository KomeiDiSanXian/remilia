package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)
	assert.NotNil(t, client.client)
	assert.NotNil(t, client.headers)
}

func TestClient_SetBaseURL(t *testing.T) {
	client := NewClient().SetBaseURL("https://api.example.com")
	assert.Equal(t, "https://api.example.com", client.baseURL)
}

func TestClient_SetTimeout(t *testing.T) {
	client := NewClient().SetTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, client.timeout)
}

func TestClient_SetHeader(t *testing.T) {
	client := NewClient().SetHeader("X-Custom", "value")
	assert.Equal(t, "value", client.headers.Get("X-Custom"))
}

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	client := NewClient()
	resp, err := client.Get(server.URL).Do()

	require.NoError(t, err)
	defer func() { _ = resp.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := resp.String()
	assert.Equal(t, "OK", body)
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))
	defer server.Close()

	client := NewClient()
	resp, err := client.Post(server.URL).
		SetJSON(map[string]string{"name": "test"}).
		Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestRequest_SetQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "value1", r.URL.Query().Get("key1"))
		assert.Equal(t, "value2", r.URL.Query().Get("key2"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient()
	resp, err := client.Get(server.URL).
		SetQuery("key1", "value1").
		SetQuery("key2", "value2").
		Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequest_SetHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "CustomAgent/1.0", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient()
	resp, err := client.Get(server.URL).
		SetHeader("Authorization", "Bearer token123").
		SetHeader("User-Agent", "CustomAgent/1.0").
		Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequest_SetJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received":true}`))
	}))
	defer server.Close()

	type TestData struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	client := NewClient()
	resp, err := client.Post(server.URL).
		SetJSON(TestData{Name: "test", Value: 42}).
		Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequest_SetForm(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		_ = r.ParseForm()
		assert.Equal(t, "value1", r.FormValue("key1"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient()
	resp, err := client.Post(server.URL).
		SetForm(map[string]string{
			"key1": "value1",
			"key2": "value2",
		}).
		Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestResponse_JSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"test","value":42}`))
	}))
	defer server.Close()

	client := NewClient()
	result, err := client.Get(server.URL).DoJSON()

	require.NoError(t, err)
	assert.Equal(t, "test", result.Get("name").String())
	assert.Equal(t, int64(42), result.Get("value").Int())
}

func TestResponse_Unmarshal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"test","value":42}`))
	}))
	defer server.Close()

	type TestData struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	client := NewClient()
	resp, err := client.Get(server.URL).Do()
	require.NoError(t, err)
	defer resp.Close()

	var data TestData
	err = resp.Unmarshal(&data)

	require.NoError(t, err)
	assert.Equal(t, "test", data.Name)
	assert.Equal(t, 42, data.Value)
}

func TestResponse_IsSuccess(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   bool
	}{
		{200, true},
		{201, true},
		{299, true},
		{300, false},
		{400, false},
		{500, false},
	}

	for _, tt := range tests {
		resp := &Response{
			Response: &http.Response{StatusCode: tt.statusCode},
		}
		assert.Equal(t, tt.expected, resp.IsSuccess(), "statusCode=%d", tt.statusCode)
	}
}

func TestResponse_IsError(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   bool
	}{
		{200, false},
		{399, false},
		{400, true},
		{404, true},
		{500, true},
	}

	for _, tt := range tests {
		resp := &Response{
			Response: &http.Response{StatusCode: tt.statusCode},
		}
		assert.Equal(t, tt.expected, resp.IsError(), "statusCode=%d", tt.statusCode)
	}
}

func TestClient_WithBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/users", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient().SetBaseURL(server.URL)
	resp, err := client.Get("/api/users").Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMiddleware_AuthBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient().Use(AuthBearerMiddleware("token123"))
	resp, err := client.Get(server.URL).Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMiddleware_UserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "CustomAgent/1.0", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient().Use(UserAgentMiddleware("CustomAgent/1.0"))
	resp, err := client.Get(server.URL).Do()

	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDefaultClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	// 测试全局便捷函数
	resp, err := Get(server.URL).Do()
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := resp.String()
	assert.Equal(t, "OK", body)
}

func BenchmarkClient_Get(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	client := NewClient()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := client.Get(server.URL).Do()
		resp.Close()
	}
}

func BenchmarkClient_PostJSON(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient()
	data := map[string]string{"key": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, _ := client.Post(server.URL).SetJSON(data).Do()
		resp.Close()
	}
}
