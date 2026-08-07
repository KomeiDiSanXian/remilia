package sauce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// ── AnimeTrace 客户端 ─────────────────────────────────────────────────
//
// AnimeTrace（https://ai.animedb.cn）是免费的以图识番引擎，支持动画与
// Galgame 图片的角色/作品识别，无需 API key。与 trace.moe 的"逐帧找番"
// 不同，AnimeTrace 擅长从人物立绘/截图识别角色与作品，两者互补。
//
// 接口：POST https://api.animetrace.com/v1/search
//   - file/url/base64 三选一上传图片
//   - is_multi=1 返回多个候选结果
//   - ai_detect=1 判定是否 AI 生成图片
//
// 状态码：17701 图片过大 / 17702 服务器繁忙（可重试）/ 17704 API 维护
// / 17728 达到使用上限 / 17731 服务利用人数过多。

// animeTraceEndpoint AnimeTrace 识别接口地址（测试中可替换为 mock 服务器）。
var animeTraceEndpoint = "https://api.animetrace.com/v1/search"

// animeTraceClient 调用 AnimeTrace 识别动画/Galgame 图片来源。
type animeTraceClient struct {
	httpClient *http.Client
}

// newAnimeTraceClient 创建 AnimeTrace 客户端。
func newAnimeTraceClient(httpClient *http.Client) *animeTraceClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &animeTraceClient{httpClient: httpClient}
}

// animeTraceResponse AnimeTrace API 响应结构。
type animeTraceResponse struct {
	Code    int             `json:"code"`    // 状态码（0 = 成功）
	Message string          `json:"message"` // 错误信息
	TraceID string          `json:"trace_id"`
	AIDet   bool            `json:"ai"` // 是否判定为 AI 生成图片
	Data    []animeTraceHit `json:"data"`
}

// animeTraceHit 单个识别结果（一个人物检测框）。
type animeTraceHit struct {
	NotConfident bool                  `json:"not_confident"` // 置信度较低（候选较多）
	Character    []animeTraceCharacter `json:"character"`     // 候选角色列表（越靠前可能性越大）
}

// animeTraceCharacter 角色候选。
type animeTraceCharacter struct {
	Work      string `json:"work"`      // 作品名称
	Character string `json:"character"` // 角色名称
}

// Search 通过 AnimeTrace 识别动画/Galgame 图片来源。
//
// 直传本地图片字节（multipart file 字段）。minConfident 为 true 时过滤
// not_confident 的低置信度命中，避免返回大量无意义候选。
func (c *animeTraceClient) Search(ctx context.Context, in engineInput, maxResults int, minConfident bool) ([]SearchResult, error) {
	if len(in.Data) == 0 {
		return nil, fmt.Errorf("AnimeTrace 需要图片字节数据")
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("is_multi", "1"); err != nil {
		return nil, err
	}
	if err := w.WriteField("ai_detect", "1"); err != nil {
		return nil, err
	}
	fw, err := w.CreateFormFile("file", "image.jpg")
	if err != nil {
		return nil, err
	}
	if _, err = fw.Write(in.Data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, animeTraceEndpoint, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var data animeTraceResponse
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if data.Code != 0 {
		return nil, fmt.Errorf("AnimeTrace 错误: %s", animeTraceErrorText(data.Code, data.Message))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AnimeTrace 返回状态 %d", resp.StatusCode)
	}

	results := make([]SearchResult, 0, len(data.Data))
	for _, hit := range data.Data {
		if minConfident && hit.NotConfident {
			continue
		}
		for _, ch := range hit.Character {
			if ch.Work == "" && ch.Character == "" {
				continue
			}
			results = append(results, SearchResult{
				Source:     "AnimeTrace",
				Title:      ch.Work,
				Author:     ch.Character,
				SourceName: "AnimeTrace",
			})
		}
	}

	return limitResults(results, maxResults), nil
}

// animeTraceErrorText 将 AnimeTrace 业务状态码转换为可读错误信息。
//
// 已知状态码（见官方 API 文档）：17701 图片过大 / 17702 服务器繁忙（可重试）
// / 17704 API 维护中 / 17705 格式不支持 / 17708 人物数量超限 /
// 17722 图片下载失败 / 17728 达到使用上限 / 17731 服务人数过多。
func animeTraceErrorText(code int, message string) string {
	if message != "" {
		return message
	}
	switch code {
	case 17701:
		return "图片大小过大"
	case 17702:
		return "服务器繁忙，请稍后重试"
	case 17704:
		return "API 维护中"
	case 17705:
		return "图片格式不支持"
	case 17708:
		return "图片中人物数量超过限制"
	case 17722:
		return "图片下载失败"
	case 17728:
		return "已达到本次使用上限"
	case 17731:
		return "服务利用人数过多，请稍后重试"
	default:
		return fmt.Sprintf("状态码 %d", code)
	}
}
