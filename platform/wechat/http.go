package wechat

import (
	"net/http"
	"time"
)

// httpClient 由发送器共享：企业微信的媒体下载与 response_url 回复都走它。
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        32,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}
