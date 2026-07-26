package health

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// APIProbe 是一个通用外部 API 健康探针。
//
// 同时实现 health.Checker 接口（用于 /health 端点）
// 和客户端侧快速失败（通过 IsHealthy 在请求前跳过不可用的 API）。
//
// 使用 StartBackground 在后台 goroutine 中定期探测。
type APIProbe struct {
	name         string
	probeURL     string
	timeout      time.Duration
	headers      http.Header
	healthy      atomic.Bool
	acceptStatus func(int) bool
	maxSeverity  Status
}

// APIProbeOption 配置 APIProbe。
type APIProbeOption func(*APIProbe)

// WithMaxSeverity 限制该探针对整体状态的最大影响级别。
// 例如设为 Degraded 时，即使探测返回 Unhealthy，整体状态最多降为 Degraded。
func WithMaxSeverity(s Status) APIProbeOption {
	return func(p *APIProbe) {
		p.maxSeverity = s
	}
}

// WithHeader 为探测请求添加自定义 HTTP 头。
// 可用于传入 Authorization、User-Agent 等。
func WithHeader(key, value string) APIProbeOption {
	return func(p *APIProbe) {
		if p.headers == nil {
			p.headers = make(http.Header)
		}
		p.headers.Set(key, value)
	}
}

// WithAcceptStatus 自定义状态码判定规则。
// fn 接收 HTTP 状态码，返回 true 表示该状态码视为 healthy。
// 默认规则: code < 500。
func WithAcceptStatus(fn func(int) bool) APIProbeOption {
	return func(p *APIProbe) {
		p.acceptStatus = fn
	}
}

// NewAPIProbe 创建一个新的 API 探针。
// name 用于 health.Checker 标识，url 为 HEAD 探测地址，timeout 为单次探测超时。
func NewAPIProbe(name, url string, timeout time.Duration, opts ...APIProbeOption) *APIProbe {
	p := &APIProbe{
		name:         name,
		probeURL:     url,
		timeout:      timeout,
		acceptStatus: func(code int) bool { return code < 500 },
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name 返回探针名称，实现 health.Checker。
func (p *APIProbe) Name() string { return p.name }

// Check 执行一次健康探测（HEAD 请求），更新内部健康状态，返回 health.CheckResult。
// 实现 health.Checker。
func (p *APIProbe) Check(ctx context.Context) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, p.probeURL, nil)
	if err != nil {
		p.healthy.Store(false)
		return p.withMaxSeverity(CheckResult{Status: Unhealthy, Error: err.Error(), Duration: time.Since(start)})
	}
	for k, vs := range p.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	d := time.Since(start)
	if err != nil {
		p.healthy.Store(false)
		return p.withMaxSeverity(CheckResult{Status: Unhealthy, Error: err.Error(), Duration: d})
	}
	resp.Body.Close()
	ok := p.acceptStatus(resp.StatusCode)
	p.healthy.Store(ok)
	metadata := map[string]any{
		"status_code":      resp.StatusCode,
		"response_time_ms": d.Milliseconds(),
	}
	result := CheckResult{Duration: d, Metadata: metadata}
	if !ok {
		result.Status = Unhealthy
		result.Error = resp.Status
	} else {
		result.Status = Healthy
	}
	return p.withMaxSeverity(result)
}

func (p *APIProbe) withMaxSeverity(r CheckResult) CheckResult {
	if p.maxSeverity != "" {
		r.MaxSeverity = p.maxSeverity
	}
	return r
}

// IsHealthy 返回最近一次探测是否表明该 API 可用。
// 供客户端代码在发起请求前快速跳过不健康的 API。
func (p *APIProbe) IsHealthy() bool { return p.healthy.Load() }

// StartBackground 在后台定期执行 Check，直到 ctx 被取消。
// interval 为探测间隔，<=0 时默认为 1 分钟。
func (p *APIProbe) StartBackground(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.Check(ctx)
		case <-ctx.Done():
			return
		}
	}
}
