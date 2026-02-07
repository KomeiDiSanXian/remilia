package httpclient

import (
	"encoding/base64"
	"time"
)

// LoggingMiddleware 日志中间件
func LoggingMiddleware(logger Logger) Middleware {
	return func(r *Request) error {
		start := time.Now()

		if logger != nil {
			logger.Infof("→ %s %s", r.method, r.url)
		}

		// 在这里无法记录响应，因为中间件在请求前执行
		// 实际的响应日志在 Do() 方法中处理

		duration := time.Since(start)
		if logger != nil {
			logger.Debugf("Request prepared in %v", duration)
		}

		return nil
	}
}

// AuthBearerMiddleware Bearer Token 认证中间件
func AuthBearerMiddleware(token string) Middleware {
	return func(r *Request) error {
		r.SetHeader("Authorization", "Bearer "+token)
		return nil
	}
}

// AuthBasicMiddleware Basic 认证中间件
func AuthBasicMiddleware(username, password string) Middleware {
	return func(r *Request) error {
		r.SetHeader("Authorization", basicAuth(username, password))
		return nil
	}
}

// UserAgentMiddleware User-Agent 中间件
func UserAgentMiddleware(userAgent string) Middleware {
	return func(r *Request) error {
		r.SetHeader("User-Agent", userAgent)
		return nil
	}
}

// TimeoutMiddleware 超时中间件
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(r *Request) error {
		if r.timeout == 0 {
			r.SetTimeout(timeout)
		}
		return nil
	}
}

// HeaderMiddleware 请求头中间件
func HeaderMiddleware(headers map[string]string) Middleware {
	return func(r *Request) error {
		for k, v := range headers {
			r.SetHeader(k, v)
		}
		return nil
	}
}

// RateLimitMiddleware 简单的速率限制中间件
type RateLimiter struct {
	lastRequest time.Time
	minInterval time.Duration
}

func NewRateLimitMiddleware(minInterval time.Duration) Middleware {
	limiter := &RateLimiter{
		minInterval: minInterval,
	}

	return func(r *Request) error {
		if !limiter.lastRequest.IsZero() {
			elapsed := time.Since(limiter.lastRequest)
			if elapsed < limiter.minInterval {
				time.Sleep(limiter.minInterval - elapsed)
			}
		}
		limiter.lastRequest = time.Now()
		return nil
	}
}

// basicAuth 生成 Basic 认证字符串
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}
