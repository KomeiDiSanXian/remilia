package sauce

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

type Plugin struct {
	saucenao *saucenaoClient
	ascii2d  *ascii2dClient
	cfg      plugin.ConfigReader
	log      plugin.Logger
}

func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "sauce",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "以图搜图，通过 SauceNAO 查找图片来源",
			Category:    "工具",
			Tags:        []string{"搜图", "SauceNAO", "以图搜图"},
			HelpText: `以图搜图 — 通过 SauceNAO 查找图片来源

用法：
  发送图片并在标题中附带 /sauce 命令

示例：
  发送图片 + 标题 /sauce`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.cfg = ctx.Config
			p.saucenao = newSauceNAOClient()
			p.ascii2d = newASCII2DClient()

			sauceDef := command.NewDef("sauce").Description("以图搜图，查找图片来源").
				Example("/sauce").Build()
			ctx.OnCommandDefWith("", "/sauce", sauceDef, p.handleSauce, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

func (p *Plugin) apiKey() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("saucenao_api_key", "")
}

func (p *Plugin) maxResults() int {
	n := 3
	if p.cfg != nil {
		n = p.cfg.GetInt("max_results", 3)
	}
	if n <= 0 {
		return 3
	}
	return n
}

func (p *Plugin) sendThumbnails() bool {
	return p.cfg != nil && p.cfg.GetBool("send_thumbnails", false)
}

func (p *Plugin) enableASCII2D() bool {
	return p.cfg != nil && p.cfg.GetBool("enable_ascii2d", false)
}

func (p *Plugin) handleSauce(ctx *eventctx.Context) error {
	imageURL := findImageURL(ctx.GetPlatformEvent())
	if imageURL == "" {
		ctx.ReplyError("请在消息中包含图片（如发送图片并在标题中附带 /sauce）"); return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx.Context(), 25*time.Second)
	defer cancel()

	ctx.ReplySuccess("正在搜索图片来源，请稍候…")

	type sourceResult struct {
		source  string
		results []SearchResult
		err     error
	}

	ch := make(chan sourceResult, 2)
	sources := 0

	sources++
	go func() {
		ak := p.apiKey()
		if ak == "" {
			ch <- sourceResult{source: "SauceNAO", err: fmt.Errorf("未配置 API Key")}
			return
		}
		results, err := p.saucenao.Search(reqCtx, ak, imageURL, p.maxResults())
		ch <- sourceResult{source: "SauceNAO", results: results, err: err}
	}()

	if p.enableASCII2D() {
		sources++
		go func() {
			results, err := p.ascii2d.Search(reqCtx, imageURL, p.maxResults())
			ch <- sourceResult{source: "ASCII2D", results: results, err: err}
		}()
	}

	var allResults []SearchResult
	for i := 0; i < sources; i++ {
		select {
		case res := <-ch:
			if res.err != nil {
				continue
			}
			allResults = append(allResults, res.results...)
		case <-reqCtx.Done():
			ctx.ReplyError("搜索超时，请稍后重试"); return nil
		}
	}

	if len(allResults) == 0 {
		ctx.ReplyText("未找到匹配结果"); return nil
	}

	results := pickResults(allResults, p.maxResults())
	sortResults(results)

	if p.sendThumbnails() {
		for i, r := range results {
			oneText := formatOneResult(r, i+1)
			if r.Thumbnail == "" {
				ctx.Reply(platform.TextMessage(oneText))
				continue
			}
			data, err := downloadImage(reqCtx, r.Thumbnail, 10*1024*1024)
			if err != nil {
				ctx.Reply(platform.TextMessage(oneText))
				continue
			}
			mimeType := detectMimeType(r.Thumbnail, data)
			ctx.Reply(platform.OutboundMessage{
				Text: oneText,
				Attachments: []platform.Attachment{{
					Kind:     platform.AttachmentKindImage,
					Data:     data,
					Name:     "sauce" + extByMime(mimeType),
					MimeType: mimeType,
				}},
			})
		}
	} else {
		ctx.Reply(platform.TextMessage(formatResults(results)))
	}

	return nil
}

func pickResults(results []SearchResult, maxResults int) []SearchResult {
	var high []SearchResult
	for _, r := range results {
		if parseSimilarity(r.Similarity) >= 80 {
			high = append(high, r)
		}
	}
	if len(high) > 0 {
		return high
	}
	if len(results) > maxResults {
		return results[:maxResults]
	}
	return results
}

func sortResults(results []SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		return parseSimilarity(results[i].Similarity) > parseSimilarity(results[j].Similarity)
	})
}

func formatResults(results []SearchResult) string {
	var b strings.Builder
	b.WriteString("🔍 图片来源搜索\n━━━━━━━━━━━━━━\n\n")
	for i, r := range results {
		b.WriteString(formatOneResult(r, i+1))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatOneResult(r SearchResult, num int) string {
	var b strings.Builder
	sim := r.Similarity
	title := r.Title
	if title == "" {
		title = "（无标题）"
	}
	if sim != "" {
		b.WriteString(fmt.Sprintf("%d. [%s%%] %s\n", num, sim, title))
	} else {
		b.WriteString(fmt.Sprintf("%d. %s\n", num, title))
	}
	if r.Author != "" {
		b.WriteString(fmt.Sprintf("   作者: %s\n", r.Author))
	}
	source := r.SourceName
	if source == "" {
		source = "未知来源"
	}
	b.WriteString(fmt.Sprintf("   来源: %s\n", source))
	for _, u := range r.ExtURLs {
		b.WriteString(fmt.Sprintf("   %s\n", u))
	}
	return strings.TrimRight(b.String(), "\n")
}

func findImageURL(event platform.Event) string {
	for _, att := range event.Attachments() {
		if att.URL == "" {
			continue
		}
		if att.MimeType != "" && !strings.HasPrefix(att.MimeType, "image/") {
			continue
		}
		return att.URL
	}
	return ""
}

func downloadImage(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("图片过大 (%d bytes)", resp.ContentLength)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func detectMimeType(url string, data []byte) string {
	lower := strings.ToLower(url)
	switch {
	case strings.Contains(lower, ".png"):
		return "image/png"
	case strings.Contains(lower, ".jpg"), strings.Contains(lower, ".jpeg"):
		return "image/jpeg"
	case strings.Contains(lower, ".gif"):
		return "image/gif"
	case strings.Contains(lower, ".webp"):
		return "image/webp"
	}
	if len(data) >= 8 {
		if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
			return "image/png"
		}
		if data[0] == 0xFF && data[1] == 0xD8 {
			return "image/jpeg"
		}
		if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
			return "image/gif"
		}
		if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
			return "image/webp"
		}
	}
	return "image/jpeg"
}

func extByMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}
