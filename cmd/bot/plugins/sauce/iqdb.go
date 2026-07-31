package sauce

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// iqdbClient 调用 IQDB（https://iqdb.org）进行多站点 booru 反向检索。
//
// IQDB 聚合 Danbooru / Konachan / yande.re / Gelbooru / Sankaku / e-shuushuu /
// Zerochan / Anime-Pictures 八个图库，其基于局部特征的匹配算法对裁切图片
// 的支持显著优于 SauceNAO。免费、无需 API key。
//
// 限制：仅支持 JPEG / PNG / GIF，最大 8MB，最大尺寸 7500x7500。
type iqdbClient struct {
	httpClient *http.Client
}

// iqdbServiceIDs 对应 IQDB 检索表单中勾选的全部服务（默认全选）。
var iqdbServiceIDs = []string{"1", "2", "3", "4", "5", "6", "11", "13"}

// maxIQDBFileSize IQDB 上传大小上限（字节）。
const maxIQDBFileSize = 8 * 1024 * 1024

// newIQDBClient 创建 IQDB 客户端。
func newIQDBClient() *iqdbClient {
	return &iqdbClient{
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// Search 通过 IQDB 搜索图片来源。
//
// 优先直传本地图片字节（multipart file 字段）；无字节数据时回退为 URL 检索。
func (c *iqdbClient) Search(ctx context.Context, in engineInput, maxResults int) ([]SearchResult, error) {
	if len(in.Data) > 0 {
		return c.searchUpload(ctx, in.Data, maxResults)
	}
	return c.searchURL(ctx, in.ImageURL, maxResults)
}

// searchUpload 以 multipart 直传图片字节执行检索。
func (c *iqdbClient) searchUpload(ctx context.Context, data []byte, maxResults int) ([]SearchResult, error) {
	if int64(len(data)) > maxIQDBFileSize {
		return nil, fmt.Errorf("IQDB 仅支持最大 8MB 的图片")
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("MAX_FILE_SIZE", "8388608"); err != nil {
		return nil, err
	}
	for _, id := range iqdbServiceIDs {
		if err := w.WriteField("service[]", id); err != nil {
			return nil, err
		}
	}
	fw, err := w.CreateFormFile("file", "image.jpg")
	if err != nil {
		return nil, err
	}
	if _, err = fw.Write(data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://iqdb.org/", body)
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IQDB 返回状态 %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	return limitResults(parseIQDBResults(raw), maxResults), nil
}

// searchURL 以远程图片 URL 执行检索（IQDB 服务端自行抓取）。
func (c *iqdbClient) searchURL(ctx context.Context, imageURL string, maxResults int) ([]SearchResult, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("无可用图片数据")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://iqdb.org/?url="+escapeURLParam(imageURL), nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IQDB 返回状态 %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	return limitResults(parseIQDBResults(raw), maxResults), nil
}

// parseIQDBResults 解析 IQDB 结果页面 HTML。
//
// 结构：<div id="pages" class="pages"> 下的子 div 为"Your image"或各站点命中，
// 命中 div 内包含 图片单元（td.image + a[href]）、站点名单元、相似度单元（N% similarity）。
func parseIQDBResults(body []byte) []SearchResult {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}

	var results []SearchResult
	visitNodes(doc, func(n *html.Node) {
		if n.DataAtom != atom.Div || getAttr(n, "id") != "pages" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.DataAtom != atom.Div || hasClass(c, "nomatch") {
				continue
			}
			if r := extractIQDBItem(c); r != nil {
				results = append(results, *r)
			}
		}
	})

	return results
}

// extractIQDBItem 从单个命中的 div 中提取结果。
func extractIQDBItem(n *html.Node) *SearchResult {
	r := &SearchResult{Source: "IQDB"}

	visitNodes(n, func(td *html.Node) {
		if td.DataAtom != atom.Td {
			return
		}

		// 相似度单元：文本形如 "97% similarity"
		text := strings.TrimSpace(getTextContent(td))
		if m := iqdbSimilarityRe.FindStringSubmatch(text); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				r.Similarity = formatSimilarity(v)
			}
			return
		}

		// 图片单元：a[href] 为来源链接，img[src] 为缩略图
		if hasClass(td, "image") {
			if a := findFirst(td, atom.A); a != nil {
				if href := getAttr(a, "href"); href != "" {
					href = normalizeResultURLRaw(href)
					r.ExtURLs = append(r.ExtURLs, href)
					if r.SourceName == "" {
						r.SourceName = sourceNameFromHost(href)
					}
				}
				if img := findFirst(a, atom.Img); img != nil {
					if src := getAttr(img, "src"); src != "" {
						r.Thumbnail = "https://iqdb.org" + src
					}
					// alt/title 中带有 Rating / Tags 描述，可用作标题
					if alt := getAttr(img, "alt"); alt != "" && r.Title == "" {
						r.Title = cleanIQDBAlt(alt)
					}
				}
			}
			return
		}

		// 站点名单元：包含 service-icon 图片的文本单元格（可能混入附加链接）
		if img := findFirst(td, atom.Img); img != nil && hasClass(img, "service-icon") {
			if r.SourceName == "" && text != "" {
				r.SourceName = firstLine(text)
			}
		}
	})

	if len(r.ExtURLs) == 0 {
		return nil
	}
	return r
}

// iqdbSimilarityRe 匹配 IQDB 相似度单元文本（如 "97% similarity"）。
var iqdbSimilarityRe = regexp.MustCompile(`([\d.]+)\s*%\s*similarity`)

// cleanIQDBAlt 从 IQDB 图片 alt 属性中提取可读标题。
func cleanIQDBAlt(alt string) string {
	parts := strings.SplitN(alt, "Tags:", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(alt)
}

// sourceNameFromHost 根据链接域名推断站点名。
func sourceNameFromHost(raw string) string {
	host := hostOf(raw)
	switch {
	case strings.Contains(host, "danbooru"):
		return "Danbooru"
	case strings.Contains(host, "konachan"):
		return "Konachan"
	case strings.Contains(host, "yande"):
		return "Yande.re"
	case strings.Contains(host, "gelbooru"):
		return "Gelbooru"
	case strings.Contains(host, "sankaku"):
		return "Sankaku"
	case strings.Contains(host, "e-shuushuu"):
		return "E-shuushuu"
	case strings.Contains(host, "zerochan"):
		return "Zerochan"
	case strings.Contains(host, "anime-pictures"):
		return "Anime-Pictures"
	}
	return host
}
