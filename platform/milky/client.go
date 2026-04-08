package milky

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// milkyClient — HTTP API 客户端
// ────────────────────────────────────────────────────────────────────────────

// milkyClient 调用 Milky HTTP API（POST /api/{endpoint}）。
type milkyClient struct {
	httpClient  *http.Client
	baseURL     string
	accessToken string
}

// newMilkyClient 创建 Milky 协议的 HTTP 客户端。
func newMilkyClient(cfg Config) *milkyClient {
	timeout := cfg.APITimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &milkyClient{
		httpClient:  &http.Client{Timeout: timeout},
		baseURL:     cfg.BaseURL,
		accessToken: cfg.AccessToken,
	}
}

// call 调用 Milky API 端点。
//
// endpoint 为动作名称（例如 "send_private_message"）。
// input 将序列化为 JSON 写入请求体。
// output 若非 nil，将从响应的 data 字段中反序列化填充。
func (c *milkyClient) call(ctx stdctx.Context, endpoint string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("milky: marshal input for %s: %w", endpoint, err)
	}

	url := c.baseURL + "/api/" + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("milky: build request for %s: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("milky: call %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("milky: %s: unauthorized (check access_token)", endpoint)
	case http.StatusNotFound:
		return fmt.Errorf("milky: %s: endpoint not found (check Milky server version)", endpoint)
	case http.StatusUnsupportedMediaType:
		return fmt.Errorf("milky: %s: unsupported media type (Content-Type must be application/json)", endpoint)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("milky: read response for %s: %w", endpoint, err)
	}

	var apiResp struct {
		Status  string          `json:"status"`
		Retcode int             `json:"retcode"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return fmt.Errorf("milky: parse response for %s: %w", endpoint, err)
	}

	if apiResp.Status != "ok" || apiResp.Retcode != 0 {
		return &apiError{
			Endpoint: endpoint,
			Retcode:  apiResp.Retcode,
			Message:  apiResp.Message,
		}
	}

	if output != nil && len(apiResp.Data) > 0 {
		if err := json.Unmarshal(apiResp.Data, output); err != nil {
			return fmt.Errorf("milky: parse data for %s: %w", endpoint, err)
		}
	}
	return nil
}

// ────────────────────────────────────────────────────────────────────────────
// apiError
// ────────────────────────────────────────────────────────────────────────────

// apiError 表示 Milky API 返回的错误。
type apiError struct {
	Endpoint string
	Retcode  int
	Message  string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("milky API error [%s] retcode=%d: %s", e.Endpoint, e.Retcode, e.Message)
}
