package telegram

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	apiBase = "https://api.telegram.org/bot"
	// fileAPIBase 是附件下载端点的前缀（与 API 端点不同路径）。
	fileAPIBase = "https://api.telegram.org/file/bot"
	// defaultMaxDownloadBytes 是 DownloadFile 的默认读取上限。
	defaultMaxDownloadBytes = 32 << 20 // 32 MiB，与 Telegram 的下载上限一致
	// defaultHTTPTimeout 是普通 API 调用的默认超时。
	defaultHTTPTimeout = 60 * time.Second
)

// Client is a lightweight Telegram Bot API client.
//
// Uses net/http directly with no external dependencies. Supports both JSON POST
// and multipart/form-data file uploads.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates a Client with the given bot token and the default timeout.
func NewClient(token string) *Client {
	return NewClientWithTimeout(token, defaultHTTPTimeout)
}

// NewClientWithTimeout creates a Client with an explicit HTTP timeout.
//
// 长轮询的调用方必须让该超时大于 getUpdates 的 timeout 参数，
// 否则客户端会在服务端正常返回之前先行中止，把健康连接误判为断线。
func NewClientWithTimeout(token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return &Client{
		baseURL: apiBase + token,
		token:   token,
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

// redactedError 包装底层错误并在格式化时抹掉 bot token。
//
// net/http 的传输错误是 *url.Error，其 Error() 会带上完整请求 URL，
// 而 Telegram 的 token 就嵌在 URL 路径里（/bot<TOKEN>/method），
// 任何一次超时/DNS 抖动都会把凭据写进日志。
// 保留 Unwrap 以便调用方的 errors.Is/As 判定不受影响。
type redactedError struct {
	err   error
	token string
}

func (e *redactedError) Error() string {
	return strings.ReplaceAll(e.err.Error(), e.token, "<redacted>")
}

func (e *redactedError) Unwrap() error { return e.err }

// redact 在错误信息中抹掉 bot token。
func (c *Client) redact(err error) error {
	if err == nil || c.token == "" {
		return err
	}
	if !strings.Contains(err.Error(), c.token) {
		return err
	}
	return &redactedError{err: err, token: c.token}
}

// APIError 表示 Telegram API 以 ok=false 返回的业务错误。
//
// 保留结构化的 Description，使调用方能够按错误内容分支处理
// （例如 parse_mode 解析失败后回退为纯文本重发），
// 而不必对拼接后的错误字符串做模糊匹配。
type APIError struct {
	// Method 是触发错误的 Bot API 方法名，如 "sendMessage"。
	Method string
	// Description 是 Telegram 返回的 description 字段原文。
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %s API error: %s", e.Method, e.Description)
}

// IsParseEntitiesError 报告错误是否为 parse_mode 富文本解析失败。
//
// Telegram 对这类错误返回形如
// "Bad Request: can't parse entities: Character '.' is reserved ..." 的描述。
// 发送方据此把消息降级为纯文本重发，避免因为一处格式字符丢失整条消息。
func IsParseEntitiesError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Description), "can't parse entities")
}

// apiResponse is the standard Telegram API response wrapper.
type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// GetUpdates polls for new updates via long polling.
//
// offset is the last processed UpdateID + 1.
// timeout is the long-polling timeout in seconds.
// limit caps how many updates to retrieve per call (max 100).
func (c *Client) GetUpdates(ctx stdctx.Context, offset int, timeout int, limit int) ([]Update, error) {
	payload := GetUpdatesPayload{
		Offset:  offset,
		Timeout: timeout,
		Limit:   limit,
	}
	if payload.Limit <= 0 {
		payload.Limit = 100
	}
	if payload.Timeout <= 0 {
		payload.Timeout = 30
	}

	var updates []Update
	if err := c.call(ctx, "getUpdates", payload, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// SendMessage sends a text message.
func (c *Client) SendMessage(ctx stdctx.Context, p *SendMessagePayload) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "sendMessage", p, &result)
	return result, err
}

// SendPhoto sends a photo by file_id or URL.
func (c *Client) SendPhoto(ctx stdctx.Context, p *SendPhotoPayload) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "sendPhoto", p, &result)
	return result, err
}

// SendAudio sends an audio file by file_id or URL.
func (c *Client) SendAudio(ctx stdctx.Context, p *SendAudioPayload) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "sendAudio", p, &result)
	return result, err
}

// SendVideo sends a video by file_id or URL.
func (c *Client) SendVideo(ctx stdctx.Context, p *SendVideoPayload) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "sendVideo", p, &result)
	return result, err
}

// SendDocument sends a document/file by file_id or URL.
func (c *Client) SendDocument(ctx stdctx.Context, p *SendDocumentPayload) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "sendDocument", p, &result)
	return result, err
}

// SendDocumentUpload sends a document with binary upload via multipart/form-data.
func (c *Client) SendDocumentUpload(ctx stdctx.Context, p *SendDocumentPayload, fileName string, data []byte) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.upload(ctx, "sendDocument", "document", fileName, data, p, &result)
	return result, err
}

// SendPhotoUpload sends a photo with binary upload via multipart/form-data.
func (c *Client) SendPhotoUpload(ctx stdctx.Context, p *SendPhotoPayload, fileName string, data []byte) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.upload(ctx, "sendPhoto", "photo", fileName, data, p, &result)
	return result, err
}

// SendAudioUpload sends audio with binary upload via multipart/form-data.
func (c *Client) SendAudioUpload(ctx stdctx.Context, p *SendAudioPayload, fileName string, data []byte) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.upload(ctx, "sendAudio", "audio", fileName, data, p, &result)
	return result, err
}

// SendVideoUpload sends video with binary upload via multipart/form-data.
func (c *Client) SendVideoUpload(ctx stdctx.Context, p *SendVideoPayload, fileName string, data []byte) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.upload(ctx, "sendVideo", "video", fileName, data, p, &result)
	return result, err
}

// EditMessageText edits a message's text.
func (c *Client) EditMessageText(ctx stdctx.Context, p *EditMessageTextPayload) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "editMessageText", p, &result)
	return result, err
}

// EditMessageReplyMarkup edits a message's inline keyboard.
func (c *Client) EditMessageReplyMarkup(ctx stdctx.Context, p *EditMessageReplyMarkupPayload) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.call(ctx, "editMessageReplyMarkup", p, &result)
	return result, err
}

// DeleteMessage deletes a message.
func (c *Client) DeleteMessage(ctx stdctx.Context, p *DeleteMessagePayload) error {
	return c.call(ctx, "deleteMessage", p, nil)
}

// SendChatAction sends a chat action indicator (typing, upload_photo, etc.).
func (c *Client) SendChatAction(ctx stdctx.Context, p *SendChatActionPayload) error {
	return c.call(ctx, "sendChatAction", p, nil)
}

// SetMessageReaction sets a reaction emoji on a message.
func (c *Client) SetMessageReaction(ctx stdctx.Context, p *SetMessageReactionPayload) error {
	return c.call(ctx, "setMessageReaction", p, nil)
}

// AnswerCallbackQuery answers an incoming callback query.
//
// If Text is non-empty, the user sees a notification popup.
// If ShowAlert is true, the popup is shown as an alert (modal) instead of a toast.
func (c *Client) AnswerCallbackQuery(ctx stdctx.Context, p *AnswerCallbackQueryPayload) error {
	return c.call(ctx, "answerCallbackQuery", p, nil)
}

// GetMe returns basic bot information (username, id, etc.).
//
// Used during adapter initialization to verify the token and fetch the bot's
// identity for BotIdentity support.
func (c *Client) GetMe(ctx stdctx.Context) (*User, error) {
	var user User
	if err := c.call(ctx, "getMe", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetFile 用 file_id 换取文件的下载信息。
//
// Telegram 的入站附件只携带不透明的 file_id，必须先调用 getFile 得到
// file_path，再拼出 https://api.telegram.org/file/bot<TOKEN>/<file_path>
// 才能真正下载。
func (c *Client) GetFile(ctx stdctx.Context, fileID string) (*File, error) {
	if fileID == "" {
		return nil, fmt.Errorf("telegram: GetFile: fileID must not be empty")
	}
	var f File
	if err := c.call(ctx, "getFile", GetFilePayload{FileID: fileID}, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// FileURL 把 getFile 返回的 file_path 拼成可直接下载的 URL。
//
// 注意：返回的 URL 路径里嵌着 bot token，属于**可直接调用 API 的活凭据**。
// 不要把它写进日志、错误信息或转发给不受信任的下游；需要记录时先经
// redactURL 处理。
func (c *Client) FileURL(filePath string) string {
	if filePath == "" {
		return ""
	}
	return fileAPIBase + c.token + "/" + filePath
}

// redactURL 抹掉 URL 中的 bot token，便于安全地记录日志。
func (c *Client) redactURL(raw string) string {
	if c.token == "" {
		return raw
	}
	return strings.ReplaceAll(raw, c.token, "<redacted>")
}

// DownloadFile 按 file_id 下载附件内容。
//
// 相比 FileURL，这个方法不会把带 token 的 URL 暴露给调用方，
// 是插件读取入站附件的推荐方式。maxBytes<=0 时使用默认上限。
func (c *Client) DownloadFile(ctx stdctx.Context, fileID string, maxBytes int64) ([]byte, error) {
	f, err := c.GetFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if f.FilePath == "" {
		return nil, fmt.Errorf("telegram: DownloadFile: empty file_path for %q", fileID)
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxDownloadBytes
	}

	rawURL := c.FileURL(f.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		// 不能带上 rawURL：其中含 token。
		return nil, fmt.Errorf("telegram: DownloadFile: create request: %w", c.redact(err))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram: DownloadFile: %w", c.redact(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram: DownloadFile: unexpected status %d for %s",
			resp.StatusCode, c.redactURL(rawURL))
	}
	// 限制读取上限，避免超大附件把进程打到 OOM。
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("telegram: DownloadFile: read body: %w", err)
	}
	return data, nil
}

// call performs a JSON POST request to the given Telegram Bot API method.
//
// params is marshaled as the JSON body. result, if non-nil, is unmarshaled
// from the "result" field of the response.
func (c *Client) call(ctx stdctx.Context, method string, params any, result any) error {
	var bodyReader io.Reader
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("telegram: marshal %s params: %w", method, err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bodyReader)
	if err != nil {
		return fmt.Errorf("telegram: create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s request failed: %w", method, c.redact(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("telegram: read %s response: %w", method, err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("telegram: parse %s response: %w", method, err)
	}

	if !apiResp.OK {
		return &APIError{Method: method, Description: apiResp.Description}
	}

	if result != nil && len(apiResp.Result) > 0 {
		if err := json.Unmarshal(apiResp.Result, result); err != nil {
			return fmt.Errorf("telegram: parse %s result: %w", method, err)
		}
	}
	return nil
}

// upload performs a multipart/form-data upload to the given Bot API method.
//
// The file is sent as the field named by fileFieldName with the given fileName.
// All fields from params are serialized as additional form fields.
// If result is non-nil, the API response "result" is unmarshaled into it.
func (c *Client) upload(ctx stdctx.Context, method string, fileFieldName, fileName string, fileData []byte, params any, result any) error {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("telegram: marshal %s params: %w", method, err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("telegram: unmarshal %s params: %w", method, err)
		}
		for key, val := range m {
			fw, err := w.CreateFormField(key)
			if err != nil {
				return fmt.Errorf("telegram: create form field %s: %w", key, err)
			}
			switch v := val.(type) {
			case string:
				if _, err := fw.Write([]byte(v)); err != nil {
					return fmt.Errorf("telegram: write form field %s: %w", key, err)
				}
			default:
				b, err := json.Marshal(v)
				if err != nil {
					return fmt.Errorf("telegram: marshal field %s: %w", key, err)
				}
				if _, err := fw.Write(b); err != nil {
					return fmt.Errorf("telegram: write form field %s: %w", key, err)
				}
			}
		}
	}

	fw, err := w.CreateFormFile(fileFieldName, fileName)
	if err != nil {
		return fmt.Errorf("telegram: create form file %s: %w", method, err)
	}
	if _, err := fw.Write(fileData); err != nil {
		return fmt.Errorf("telegram: write file %s: %w", method, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("telegram: close multipart writer %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, &b)
	if err != nil {
		return fmt.Errorf("telegram: create %s upload request: %w", method, err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s upload failed: %w", method, c.redact(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("telegram: read %s upload response: %w", method, err)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("telegram: parse %s upload response: %w", method, err)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram: %s upload API error: %s", method, apiResp.Description)
	}
	if result != nil && len(apiResp.Result) > 0 {
		if err := json.Unmarshal(apiResp.Result, result); err != nil {
			return fmt.Errorf("telegram: parse %s upload result: %w", method, err)
		}
	}
	return nil
}
