// Package aimage client.go — 图片生成后端客户端。
//
// 支持两种后端：
//   - openai：OpenAI 兼容 /images/generations 端点（DALL-E、SiliconFlow 等中转）
//   - sdwebui：本地 Stable Diffusion WebUI /sdapi/v1/txt2img（AUTOMATIC1111 风格）
//
// 所有后端结果统一物化为二进制（解码 base64 或下载 URL），
// 发送不再依赖远程 URL（规避过期/SSRF 问题）。
package aimage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/infra/outbound"
)

const (
	// providerOpenAI OpenAI 兼容 /images/generations 后端。
	providerOpenAI = "openai"
	// providerSDWebUI Stable Diffusion WebUI /sdapi/v1/txt2img 后端。
	providerSDWebUI = "sdwebui"

	// maxImageBytes 单张生成图片的体积上限（15MB）。
	maxImageBytes = 15 * 1024 * 1024
)

// imageResult 一张已物化的生成图片。
type imageResult struct {
	Data     []byte
	MimeType string
}

// imageClient 图片生成后端客户端。
//
// api 调用生成端点（base_url 为管理员配置的固定端点 → OperatorEndpoint）；
// download 下载生成结果 URL（URL 来自 API 响应，属半可信输入）：命中所配置
// base_url 同源的地址视为受信（本地/内网后端常把自己托管的图片地址回给
// 调用方，如 http://127.0.0.1:7860/outputs/x.png），其余目标仍按
// "https + 公网" 准入，并逐跳校验重定向。
type imageClient struct {
	provider string
	baseURL  string
	apiKey   string
	model    string
	size     string
	steps    int
	cfgScale float64
	negative string
	timeout  time.Duration
	api      *outbound.Client
	download *outbound.Client
	// backend 解析后的 base_url（相对地址解析用）；无法解析时为 nil。
	backend *url.URL
}

// newImageClient 创建图片生成客户端。baseURL 为空或 provider 未知时返回错误。
func newImageClient(provider, baseURL, apiKey, model, size string, steps int, cfgScale float64, negative string, timeout time.Duration, proxy string) (*imageClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base_url 未配置")
	}
	if provider != providerOpenAI && provider != providerSDWebUI {
		return nil, fmt.Errorf("未知 provider %q（支持 openai / sdwebui）", provider)
	}
	// base_url 由管理员配置：生成端点与同源图片地址都受信，可位于内网/明文。
	api, err := outbound.New(outbound.Policy{
		Component:    "aimage.api",
		Class:        outbound.OperatorEndpoint,
		Origins:      []string{baseURL},
		Proxy:        proxy,
		Timeout:      timeout,
		MaxRedirects: 3,
	})
	if err != nil {
		return nil, err
	}
	download, err := outbound.New(outbound.Policy{
		Component:    "aimage.download",
		Class:        outbound.TrustedOrigin,
		Origins:      []string{baseURL},
		Proxy:        proxy,
		Timeout:      timeout,
		MaxRedirects: 10,
	})
	if err != nil {
		return nil, err
	}
	backend, _ := url.Parse(baseURL)
	return &imageClient{
		provider: provider,
		baseURL:  baseURL,
		apiKey:   apiKey,
		model:    model,
		size:     size,
		steps:    steps,
		cfgScale: cfgScale,
		negative: negative,
		timeout:  timeout,
		api:      api,
		download: download,
		backend:  backend,
	}, nil
}

// generate 调用后端生成 n 张图片并物化为二进制。
func (c *imageClient) generate(ctx context.Context, prompt string, n int) ([]imageResult, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt 不能为空")
	}
	switch c.provider {
	case providerOpenAI:
		return c.generateOpenAI(ctx, prompt, n)
	case providerSDWebUI:
		return c.generateSDWebUI(ctx, prompt, n)
	default:
		return nil, fmt.Errorf("未知 provider %q", c.provider)
	}
}

// ── OpenAI 兼容 /images/generations ────────────────────────────────────

type openAIGenRequest struct {
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt"`
	N      int    `json:"n,omitempty"`
	Size   string `json:"size,omitempty"`
}

type openAIGenResponse struct {
	Data []struct {
		URL     string `json:"url"`
		B64JSON string `json:"b64_json"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *imageClient) generateOpenAI(ctx context.Context, prompt string, n int) ([]imageResult, error) {
	body, err := json.Marshal(openAIGenRequest{Model: c.model, Prompt: prompt, N: n, Size: c.size})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/images/generations", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	data, err := doJSON(c.api, req, maxImageBytes*4)
	if err != nil {
		return nil, fmt.Errorf("调用图片生成 API 失败: %w", err)
	}
	var out openAIGenResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析图片生成响应失败: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, fmt.Errorf("图片生成 API 错误: %s", out.Error.Message)
	}

	images := make([]imageResult, 0, len(out.Data))
	for _, d := range out.Data {
		img, err := c.materialize(ctx, d.B64JSON, d.URL)
		if err != nil {
			logger.Warnf("[aimage] 物化生成图片失败: %v", err)
			continue
		}
		images = append(images, img)
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("图片生成服务未返回可用图片")
	}
	return images, nil
}

// ── Stable Diffusion WebUI /sdapi/v1/txt2img ───────────────────────────

type sdWebUIRequest struct {
	Prompt         string  `json:"prompt"`
	NegativePrompt string  `json:"negative_prompt"`
	Steps          int     `json:"steps"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	CfgScale       float64 `json:"cfg_scale"`
}

type sdWebUIResponse struct {
	Images []string `json:"images"`
}

func (c *imageClient) generateSDWebUI(ctx context.Context, prompt string, n int) ([]imageResult, error) {
	width, height, err := parseSize(c.size)
	if err != nil {
		return nil, fmt.Errorf("无效的 size %q（应为 宽x高，如 1024x1024）: %w", c.size, err)
	}
	images := make([]imageResult, 0, n)
	for i := range n {
		body, err := json.Marshal(sdWebUIRequest{
			Prompt:         prompt,
			NegativePrompt: c.negative,
			Steps:          c.steps,
			Width:          width,
			Height:         height,
			CfgScale:       c.cfgScale,
		})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/sdapi/v1/txt2img", strings.NewReader(string(body)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		data, err := doJSON(c.api, req, maxImageBytes*4)
		if err != nil {
			return nil, fmt.Errorf("调用 SD WebUI 失败（第 %d 张）: %w", i+1, err)
		}
		var out sdWebUIResponse
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("解析 SD WebUI 响应失败（第 %d 张）: %w", i+1, err)
		}
		if len(out.Images) == 0 {
			return nil, fmt.Errorf("SD WebUI 未返回图片（第 %d 张）", i+1)
		}
		for _, b64 := range out.Images {
			img, err := c.materialize(ctx, b64, "")
			if err != nil {
				return nil, fmt.Errorf("解码 SD WebUI 图片失败: %w", err)
			}
			images = append(images, img)
		}
	}
	return images, nil
}

// ── 结果物化 / 工具函数 ────────────────────────────────────────────────

// materialize 将后端返回的 base64 或 URL 物化为二进制图片。
func (c *imageClient) materialize(ctx context.Context, b64, imageURL string) (imageResult, error) {
	if b64 != "" {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return imageResult{}, fmt.Errorf("base64 解码失败: %w", err)
		}
		if len(data) > maxImageBytes {
			return imageResult{}, fmt.Errorf("图片过大（%d bytes）", len(data))
		}
		return imageResult{Data: data, MimeType: detectImageMime(data)}, nil
	}
	if imageURL == "" {
		return imageResult{}, fmt.Errorf("响应中无图片数据")
	}
	imageURL = c.normalizeImageURL(imageURL)
	if err := c.download.Allow(imageURL); err != nil {
		return imageResult{}, fmt.Errorf("图片 URL 不被允许下载: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return imageResult{}, err
	}
	resp, err := c.download.Do(req)
	if err != nil {
		return imageResult{}, fmt.Errorf("下载图片失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return imageResult{}, fmt.Errorf("下载图片失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return imageResult{}, err
	}
	if len(data) > maxImageBytes {
		return imageResult{}, fmt.Errorf("图片过大（%d bytes）", len(data))
	}
	return imageResult{Data: data, MimeType: detectImageMime(data)}, nil
}

// normalizeImageURL 归一化后端返回的图片地址：相对路径按 base_url 解析
// （本地后端常返回 /outputs/x.png 这类相对地址）。是否受信由下载策略判定
// （受信来源 = base_url 同源，见 newImageClient）。
func (c *imageClient) normalizeImageURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	if c.backend != nil {
		u = c.backend.ResolveReference(u)
	}
	return u.String()
}

// doer 是出站客户端的最小接口（*outbound.Client 与 *http.Client 都满足）。
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// doJSON 发送请求并读取响应体（上限 maxBytes），非 2xx 时附带状态码报错。
func doJSON(client doer, req *http.Request, maxBytes int64) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		detail := strings.TrimSpace(string(msg))
		if detail == "" {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateRunes(detail, 300))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// parseSize 解析 "宽x高" 尺寸字符串（如 "1024x1024"）。
func parseSize(size string) (width, height int, err error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(size)), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("格式应为 宽x高")
	}
	width, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || width <= 0 {
		return 0, 0, fmt.Errorf("无效宽度 %q", parts[0])
	}
	height, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || height <= 0 {
		return 0, 0, fmt.Errorf("无效高度 %q", parts[1])
	}
	return width, height, nil
}

// detectImageMime 通过 magic bytes 嗅探图片 MIME 类型；未知时默认 image/png。
func detectImageMime(data []byte) string {
	if ct := http.DetectContentType(data); strings.HasPrefix(ct, "image/") {
		return ct
	}
	return "image/png"
}

// truncateRunes 截断字符串到 max 个 rune（用于错误信息防撑爆）。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
