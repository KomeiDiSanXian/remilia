package telegram

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const apiBase = "https://api.telegram.org/bot"

// Client is a lightweight Telegram Bot API client.
//
// Uses net/http directly with no external dependencies. Supports both JSON POST
// and multipart/form-data file uploads.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a Client with the given bot token.
func NewClient(token string) *Client {
	return &Client{
		baseURL: apiBase + token,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
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
		return fmt.Errorf("telegram: %s request failed: %w", method, err)
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
		return fmt.Errorf("telegram: %s API error: %s", method, apiResp.Description)
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
		return fmt.Errorf("telegram: %s upload failed: %w", method, err)
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
