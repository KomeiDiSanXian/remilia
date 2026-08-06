package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/tidwall/gjson"
)

// Client 是一个增强的 HTTP 客户端
type Client struct {
	client      *http.Client
	baseURL     string
	headers     http.Header
	timeout     time.Duration
	middlewares []Middleware
	retryConfig *RetryConfig
	logger      Logger
}

// Request 表示一个 HTTP 请求
type Request struct {
	client      *Client
	method      string
	url         string
	headers     http.Header
	body        io.Reader
	timeout     time.Duration
	context     context.Context
	middlewares []Middleware
	// 修复 #9：buildErr 存储构建阶段（SetJSON 等）产生的错误，在 Do() 时统一返回，
	// 避免静默丢弃导致调用方收到空 body 请求但不知原因。
	buildErr error
}

// Response 包装 http.Response 并提供便捷方法
type Response struct {
	*http.Response
	body []byte
}

// Middleware 定义中间件函数类型
type Middleware func(*Request) error

// Logger 定义日志接口
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries     int
	RetryWaitTime  time.Duration
	RetryMaxWait   time.Duration
	RetryCondition func(*http.Response, error) bool
}

// DefaultTransportConfig 默认连接池配置
var DefaultTransportConfig = TransportConfig{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 10,
	MaxConnsPerHost:     0, // 0 表示不限制
	IdleConnTimeout:     90 * time.Second,
	DisableKeepAlives:   false,
}

// TransportConfig 连接池配置
type TransportConfig struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration
	DisableKeepAlives   bool
}

// NewClient 创建一个新的 HTTP 客户端
//
// 每个 Client 拥有独立的连接池，避免与其他 Client 实例互相干扰。
// 默认配置：MaxIdleConns=100, MaxIdleConnsPerHost=10, IdleConnTimeout=90s
func NewClient() *Client {
	return NewClientWithTransport(DefaultTransportConfig)
}

// NewClientWithTransport 创建带自定义连接池配置的 HTTP 客户端
func NewClientWithTransport(tc TransportConfig) *Client {
	transport := &http.Transport{
		MaxIdleConns:        tc.MaxIdleConns,
		MaxIdleConnsPerHost: tc.MaxIdleConnsPerHost,
		MaxConnsPerHost:     tc.MaxConnsPerHost,
		IdleConnTimeout:     tc.IdleConnTimeout,
		DisableKeepAlives:   tc.DisableKeepAlives,
	}
	return &Client{
		client:  &http.Client{Transport: transport},
		headers: make(http.Header),
	}
}

// SetBaseURL 设置基础 URL
func (c *Client) SetBaseURL(baseURL string) *Client {
	c.baseURL = baseURL
	return c
}

// SetTimeout 设置默认超时时间
func (c *Client) SetTimeout(timeout time.Duration) *Client {
	c.timeout = timeout
	c.client.Timeout = timeout
	return c
}

// SetHeader 设置默认请求头
func (c *Client) SetHeader(key, value string) *Client {
	c.headers.Set(key, value)
	return c
}

// SetHeaders 批量设置请求头
func (c *Client) SetHeaders(headers map[string]string) *Client {
	for k, v := range headers {
		c.headers.Set(k, v)
	}
	return c
}

// SetHTTPClient 设置底层 http.Client
func (c *Client) SetHTTPClient(client *http.Client) *Client {
	c.client = client
	return c
}

// SetLogger 设置日志记录器
func (c *Client) SetLogger(logger Logger) *Client {
	c.logger = logger
	return c
}

// SetRetry 设置重试配置
func (c *Client) SetRetry(config *RetryConfig) *Client {
	c.retryConfig = config
	return c
}

// Use 添加中间件
func (c *Client) Use(middleware Middleware) *Client {
	c.middlewares = append(c.middlewares, middleware)
	return c
}

// Get 创建 GET 请求
func (c *Client) Get(url string) *Request {
	return c.NewRequest(http.MethodGet, url)
}

// Post 创建 POST 请求
func (c *Client) Post(url string) *Request {
	return c.NewRequest(http.MethodPost, url)
}

// Put 创建 PUT 请求
func (c *Client) Put(url string) *Request {
	return c.NewRequest(http.MethodPut, url)
}

// Delete 创建 DELETE 请求
func (c *Client) Delete(url string) *Request {
	return c.NewRequest(http.MethodDelete, url)
}

// Patch 创建 PATCH 请求
func (c *Client) Patch(url string) *Request {
	return c.NewRequest(http.MethodPatch, url)
}

// Head 创建 HEAD 请求
func (c *Client) Head(url string) *Request {
	return c.NewRequest(http.MethodHead, url)
}

// Options 创建 OPTIONS 请求
func (c *Client) Options(url string) *Request {
	return c.NewRequest(http.MethodOptions, url)
}

// NewRequest 创建新请求
func (c *Client) NewRequest(method, urlStr string) *Request {
	// 处理 baseURL
	fullURL := urlStr
	if c.baseURL != "" {
		if u, err := url.Parse(c.baseURL); err == nil {
			if relURL, err := url.Parse(urlStr); err == nil {
				fullURL = u.ResolveReference(relURL).String()
			}
		}
	}

	req := &Request{
		client:  c,
		method:  method,
		url:     fullURL,
		headers: c.headers.Clone(),
		timeout: c.timeout,
		context: context.Background(),
		// 修复 #10：拷贝 middlewares 切片，避免多个并发 Request.Use() 共享底层数组引发竞态
		middlewares: append([]Middleware(nil), c.middlewares...),
	}

	return req
}

// SetHeader 设置请求头
func (r *Request) SetHeader(key, value string) *Request {
	if r.headers == nil {
		r.headers = make(http.Header)
	}
	r.headers.Set(key, value)
	return r
}

// SetHeaders 批量设置请求头
func (r *Request) SetHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.SetHeader(k, v)
	}
	return r
}

// SetBody 设置请求体
func (r *Request) SetBody(body io.Reader) *Request {
	r.body = body
	return r
}

// SetJSON 设置 JSON 请求体
func (r *Request) SetJSON(data any) *Request {
	jsonData, err := json.Marshal(data)
	if err != nil {
		// 修复 #9：存储错误，Do() 时返回，而非静默丢弃（避免发出空 body 请求）
		r.buildErr = fmt.Errorf("SetJSON: failed to marshal data: %w", err)
		return r
	}
	r.body = bytes.NewReader(jsonData)
	r.SetHeader("Content-Type", "application/json")
	return r
}

// SetForm 设置表单数据
func (r *Request) SetForm(data map[string]string) *Request {
	form := url.Values{}
	for k, v := range data {
		form.Set(k, v)
	}
	r.body = bytes.NewReader([]byte(form.Encode()))
	r.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// SetQuery 设置查询参数
func (r *Request) SetQuery(key, value string) *Request {
	u, err := url.Parse(r.url)
	if err != nil {
		return r
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	r.url = u.String()
	return r
}

// SetQueries 批量设置查询参数
func (r *Request) SetQueries(queries map[string]string) *Request {
	for k, v := range queries {
		r.SetQuery(k, v)
	}
	return r
}

// AddQuery 添加查询参数（允许重复键）
func (r *Request) AddQuery(key, value string) *Request {
	u, err := url.Parse(r.url)
	if err != nil {
		return r
	}
	q := u.Query()
	q.Add(key, value)
	u.RawQuery = q.Encode()
	r.url = u.String()
	return r
}

// SetTimeout 设置请求超时
func (r *Request) SetTimeout(timeout time.Duration) *Request {
	r.timeout = timeout
	return r
}

// SetContext 设置请求上下文
func (r *Request) SetContext(ctx context.Context) *Request {
	r.context = ctx
	return r
}

// Use 为单个请求添加中间件
func (r *Request) Use(middleware Middleware) *Request {
	r.middlewares = append(r.middlewares, middleware)
	return r
}

// Do 执行请求
//
// 重要：调用者必须在使用完 Response 后调用 resp.Close() 关闭响应体，
// 否则会导致 HTTP 连接泄漏。
//
// 示例：
//
//	resp, err := client.Get(url).Do()
//	if err != nil {
//	    return err
//	}
//	defer resp.Close()  // 必须关闭！
//
//	body, _ := resp.Bytes()
//
// 如果不想手动管理 Response 关闭，可以使用便捷方法：
// - DoJSON()  自动关闭并解析 JSON
// - DoString() 自动关闭并返回字符串
// - DoBytes()  自动关闭并返回字节数组
func (r *Request) Do() (*Response, error) {
	// 修复 #9：返回构建阶段（如 SetJSON）存储的错误
	if r.buildErr != nil {
		return nil, r.buildErr
	}

	// 执行中间件
	for _, mw := range r.middlewares {
		if err := mw(r); err != nil {
			return nil, fmt.Errorf("middleware error: %w", err)
		}
	}

	// 创建 HTTP 请求
	ctx := r.context
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(ctx, r.method, r.url, r.body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header = r.headers

	// 记录请求日志
	if r.client.logger != nil {
		r.client.logger.Debugf("HTTP %s %s", r.method, r.url)
	}

	// 执行请求（可能带重试）
	var resp *http.Response
	if r.client.retryConfig != nil {
		resp, err = r.doWithRetry(httpReq)
	} else {
		resp, err = r.client.client.Do(httpReq)
	}

	if err != nil {
		if r.client.logger != nil {
			r.client.logger.Errorf("HTTP request failed: %v", err)
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// 记录响应日志
	if r.client.logger != nil {
		r.client.logger.Debugf("HTTP %d %s", resp.StatusCode, r.url)
	}

	return &Response{Response: resp}, nil
}

// doWithRetry 执行带重试的请求
//
// 修复：预先将请求体读入内存，每次重试时创建新的 reader，
// 避免非 io.Seeker 的请求体（如 strings.NewReader）在重试时发送空 body。
func (r *Request) doWithRetry(req *http.Request) (*http.Response, error) {
	config := r.client.retryConfig
	var resp *http.Response
	var err error

	// 预先读取请求体，以便每次重试都能重新发送完整的 body
	var bodyBytes []byte
	if r.body != nil {
		bodyBytes, err = io.ReadAll(r.body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body for retry: %w", err)
		}
	}

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			// 等待后重试
			waitTime := min(config.RetryWaitTime*time.Duration(attempt), config.RetryMaxWait)

			if r.client.logger != nil {
				r.client.logger.Infof("Retrying request (attempt %d/%d) after %v",
					attempt, config.MaxRetries, waitTime)
			}

			// 修复 B8：使用 select 监听 ctx.Done()，确保 context 取消时立即响应。
			// 使用 time.NewTimer 替代 time.After：context 取消时立即 Stop timer，
			// 避免 time.After 创建的 timer 在等待期间无法被提前回收，
			// 减少高并发场景下的 timer 累积内存压力。
			retryTimer := time.NewTimer(waitTime)
			select {
			case <-retryTimer.C:
			case <-req.Context().Done():
				retryTimer.Stop()
				return nil, req.Context().Err()
			}

			// 重新创建 HTTP 请求以重置请求体（避免 consumed reader 问题）
			var reqBody io.Reader
			if bodyBytes != nil {
				reqBody = bytes.NewReader(bodyBytes)
			}
			req, err = http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), reqBody)
			if err != nil {
				return nil, fmt.Errorf("failed to recreate request for retry: %w", err)
			}
			req.Header = r.headers
		}

		resp, err = r.client.client.Do(req)

		// 检查是否需要重试
		if config.RetryCondition != nil && config.RetryCondition(resp, err) {
			if resp != nil {
				_ = resp.Body.Close()
			}
			continue
		}

		// 成功或不需要重试
		break
	}

	return resp, err
}

// DoJSON 执行请求并解析 JSON 响应
func (r *Request) DoJSON() (gjson.Result, error) {
	resp, err := r.Do()
	if err != nil {
		return gjson.Result{}, err
	}
	defer func() { _ = resp.Close() }()

	body, err := resp.Bytes()
	if err != nil {
		return gjson.Result{}, err
	}

	return gjson.ParseBytes(body), nil
}

// DoString 执行请求并返回字符串响应
func (r *Request) DoString() (string, error) {
	resp, err := r.Do()
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Close() }()

	body, err := resp.Bytes()
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// DoBytes 执行请求并返回字节响应
func (r *Request) DoBytes() ([]byte, error) {
	resp, err := r.Do()
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Close() }()

	return resp.Bytes()
}

// Bytes 读取响应体为字节数组
//
// 首次调用时读取并缓存响应体，后续调用直接返回缓存。
// 注意：读取后 Body 会被关闭，应使用 Close() 方法手动关闭（Close 内部做幂等处理）。
func (r *Response) Bytes() ([]byte, error) {
	if r.body != nil {
		return r.body, nil
	}

	body, err := io.ReadAll(r.Body)
	// 无论读取成功与否，关闭 Body 释放连接
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}

	r.body = body
	return body, nil
}

// String 读取响应体为字符串
func (r *Response) String() (string, error) {
	body, err := r.Bytes()
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// JSON 解析响应体为 JSON
func (r *Response) JSON() (gjson.Result, error) {
	body, err := r.Bytes()
	if err != nil {
		return gjson.Result{}, err
	}
	return gjson.ParseBytes(body), nil
}

// Unmarshal 将响应体反序列化到指定对象
func (r *Response) Unmarshal(v any) error {
	body, err := r.Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// IsSuccess 检查响应是否成功 (2xx)
func (r *Response) IsSuccess() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}

// IsError 检查响应是否为错误 (4xx or 5xx)
func (r *Response) IsError() bool {
	return r.StatusCode >= 400
}

// Close 关闭响应体
func (r *Response) Close() error {
	if r.Body != nil {
		return r.Body.Close()
	}
	return nil
}

// 全局默认客户端
var defaultClient = NewClient()

// SetDefaultHTTPClient 替换全局默认客户端的底层 http.Client，并返回被替换的旧值。
//
// 主要供测试使用：将默认客户端指向本地 httptest.Server，即可为使用包级
// 便捷函数（httpclient.Post/Get/...）的代码提供可控的伪 HTTP 环境；
// 测试结束时应恢复旧值：
//
//	old := httpclient.SetDefaultHTTPClient(server.Client())
//	defer httpclient.SetDefaultHTTPClient(old)
func SetDefaultHTTPClient(client *http.Client) *http.Client {
	old := defaultClient.client
	defaultClient.client = client
	return old
}

// Get 使用默认客户端创建 GET 请求
func Get(url string) *Request {
	return defaultClient.Get(url)
}

// Post 使用默认客户端创建 POST 请求
func Post(url string) *Request {
	return defaultClient.Post(url)
}

// Put 使用默认客户端创建 PUT 请求
func Put(url string) *Request {
	return defaultClient.Put(url)
}

// Patch 使用默认客户端创建 PATCH 请求
func Patch(url string) *Request {
	return defaultClient.Patch(url)
}

// Delete 使用默认客户端创建 DELETE 请求
func Delete(url string) *Request {
	return defaultClient.Delete(url)
}

// DefaultRetryCondition 默认重试条件：网络错误或 5xx 错误
func DefaultRetryCondition(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	if resp != nil && resp.StatusCode >= 500 {
		return true
	}
	return false
}
