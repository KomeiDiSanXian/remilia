// retry.go — 传输层重试循环（OpenAI/Anthropic 两个适配器共用的机制）。
//
// 语义：
//   - 单次请求超时：perAttemptTimeout 即 ProviderOptions.APITimeout 的文档
//     语义"单次请求超时"，每轮独立计时——此前超时挂在整个重试循环外，
//     重试退避挤占最后一次请求的额度，长回复被莫名截断。
//   - 瞬态错误重试：429（限流，多群并发下的常态）、408（请求超时）与 5xx；
//     429 优先按 Retry-After 头等待。其余 4xx（400/401/403/404）是客户端
//     错误，重试不会变好，原样上抛。
//   - 错误体读取限制在 1MB：恶意/异常网关可返回超大错误体打爆内存。
package protocol

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxErrorBodyBytes 读取错误响应体的上限。
const maxErrorBodyBytes = 1 << 20

// maxResponseBytes 读取正常响应体的上限（防止恶意/异常网关返回超大载荷）。
const maxResponseBytes = 32 << 20

// maxRetryWait 单次重试等待上限（Retry-After 过大时收敛，避免回合无限挂起）。
const maxRetryWait = 30 * time.Second

// defaultAttemptTimeout 未配置单次超时时的默认值。
const defaultAttemptTimeout = 60 * time.Second

// APIError 携带 HTTP 状态码与 Retry-After 的 LLM API 错误。
// 编排层按 errors.As 精确映射用户提示，避免对错误串做子串匹配。
type APIError struct {
	// StatusCode HTTP 状态码。
	StatusCode int
	// Body 响应体（截断到 maxErrorBodyBytes）。
	Body string
	// RetryAfter 服务端建议的等待时长（Retry-After 头；缺失为 0）。
	RetryAfter time.Duration
}

// Error 实现 error。
func (e *APIError) Error() string {
	if e.Body != "" {
		return "ai: api returned " + strconv.Itoa(e.StatusCode) + ": " + e.Body
	}
	return "ai: api returned " + strconv.Itoa(e.StatusCode)
}

// parseRetryAfter 解析 Retry-After 头（秒数或 HTTP 日期）；非法/缺失返回 0。
func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// retryableStatus 判断状态码是否值得重试（429/408/5xx）。
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusRequestTimeout:
		return true
	}
	return code >= 500
}

// canceledBody 在 Body 关闭时回收每轮的 context，消除 context 泄漏。
type canceledBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

// Close 先取消 context 再关闭底层 Body。
func (c *canceledBody) Close() error {
	c.cancel()
	return c.ReadCloser.Close()
}

// executeWithRetry 执行带退避重试的请求循环。返回首个"不该重试"的响应
// （2xx 与非瞬态 4xx 原样返回，由调用方解析或报错；429/408/5xx 在此重试，
// 429 的等待时长优先取 Retry-After 头）。每次重试都重建请求体
// （http.Request.Body 是一次性读取的）。
func executeWithRetry(
	ctx context.Context,
	maxRetries int,
	perAttemptTimeout time.Duration,
	buildReq func() (*http.Request, error),
	client *http.Client,
) (*http.Response, error) {
	if perAttemptTimeout <= 0 {
		perAttemptTimeout = defaultAttemptTimeout
	}
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * time.Second
			var ae *APIError
			if errors.As(lastErr, &ae) && ae.RetryAfter > 0 {
				wait = min(ae.RetryAfter, maxRetryWait)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		httpReq, err := buildReq()
		if err != nil {
			return nil, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, perAttemptTimeout)
		resp, err := client.Do(httpReq.WithContext(attemptCtx))
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		if retryableStatus(resp.StatusCode) {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
			resp.Body.Close()
			cancel()
			lastErr = &APIError{
				StatusCode: resp.StatusCode,
				Body:       string(errBody),
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
			continue
		}
		// Body 关闭时回收本轮 context。
		resp.Body = &canceledBody{ReadCloser: resp.Body, cancel: cancel}
		return resp, nil
	}
	if lastErr != nil {
		return nil, errors.New("ai: http request failed after retries: " + lastErr.Error())
	}
	return nil, errors.New("ai: unexpected retry exit")
}
