package sauce

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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
// 服务高峰期需要排队，可能出现超时或明显延迟，Search 内对瞬时失败做
// 有限次重试（重试次数与单次超时由配置控制）。
type iqdbClient struct {
	httpClient *http.Client
	retries    int
}

// iqdbEndpoint IQDB 检索端点（测试中可替换为 mock 服务器）。
var iqdbEndpoint = "https://iqdb.org/"

// iqdbServiceIDs 对应 IQDB 检索表单中勾选的全部服务（默认全选）。
var iqdbServiceIDs = []string{"1", "2", "3", "4", "5", "6", "11", "13"}

// maxIQDBFileSize IQDB 上传大小上限（字节）。
const maxIQDBFileSize = 8 * 1024 * 1024

// newIQDBClient 创建 IQDB 客户端。
//
// httpClient 为共享 Transport 上的客户端（超时已按 iqdb_timeout 配置）；
// retries 为失败重试次数（默认 1）。
func newIQDBClient(httpClient *http.Client, retries int) *iqdbClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	if retries < 0 {
		retries = 0
	}
	return &iqdbClient{
		httpClient: httpClient,
		retries:    retries,
	}
}

// iqdbRetryableError 标记可重试的 IQDB 失败（超时/连接错误/5xx 排队满载）。
type iqdbRetryableError struct {
	msg string
	err error
}

func (e *iqdbRetryableError) Error() string { return e.msg }
func (e *iqdbRetryableError) Unwrap() error { return e.err }

// iqdbStatusError 将非 200 状态码转换为错误。
// 5xx（排队满载等瞬时失败）包装为可重试错误；4xx 直接返回不可重试错误。
func iqdbStatusError(statusCode int) error {
	err := fmt.Errorf("status %d", statusCode)
	if statusCode >= 500 {
		return &iqdbRetryableError{
			msg: fmt.Sprintf("IQDB 返回状态 %d", statusCode),
			err: err,
		}
	}
	return err
}

// iqdbQueuedError 表示 IQDB 正在长排队（高峰期常见，等待时间不可控）。
type iqdbQueuedError struct {
	position int
}

func (e *iqdbQueuedError) Error() string {
	return fmt.Sprintf("IQDB 正在排队（队列位置 %d），请稍后重试", e.position)
}

// iqdbQueueRe 匹配 IQDB 排队标记脚本：queue('N','0')。
// 实测（2026-08）：高峰期响应页逐位置回传排队进度，N 为当前队列位置；
// 队列过长时单次请求可达 60s+ 仍未轮到，此时应明确提示排队而非静默超时。
var iqdbQueueRe = regexp.MustCompile(`queue\('(\d+)','(\d+)'\)`)

// iqdbQueuedPosition 从响应 HTML 中提取排队位置。
//
// 返回 (position, wait, true)：
//   - position>0：队列中第 N 个（wait=0）
//   - position=0 && wait=1：正在处理（无需排队）
//   - 无匹配标记：返回 0, 0, false
func iqdbQueuedPosition(body []byte) (int, int, bool) {
	// 只统计页面末尾的最新进度：页面内按时间顺序回传多个 queue 标记，
	// 取最后一个为当前状态。
	var pos, wait int
	var found bool
	for _, m := range iqdbQueueRe.FindAllSubmatch(body, -1) {
		if len(m) == 3 {
			if v, err := strconv.Atoi(string(m[1])); err == nil {
				pos = v
				found = true
			}
			if v, err := strconv.Atoi(string(m[2])); err == nil {
				wait = v
			}
		}
	}
	return pos, wait, found
}

// Search 通过 IQDB 搜索图片来源。
//
// 优先直传本地图片字节（multipart file 字段）；无字节数据时回退为 URL 检索。
// 瞬时失败（超时/连接错误/5xx/长排队）时按配置重试，重试间短暂退避。
func (c *iqdbClient) Search(ctx context.Context, in engineInput, maxResults int) ([]SearchResult, error) {
	search := func() ([]SearchResult, error) {
		if len(in.Data) > 0 {
			return c.searchUpload(ctx, in.Data, maxResults)
		}
		return c.searchURL(ctx, in.ImageURL, maxResults)
	}

	results, err := search()
	for attempt := 0; err != nil && attempt < c.retries; attempt++ {
		var re *iqdbRetryableError
		var qe *iqdbQueuedError
		if !errors.As(err, &re) && !errors.As(err, &qe) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
		}
		results, err = search()
	}
	return results, err
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, iqdbEndpoint, body)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", w.FormDataContentType())

	return c.doSearch(ctx, req, maxResults)
}

// searchURL 以远程图片 URL 执行检索（IQDB 服务端自行抓取）。
func (c *iqdbClient) searchURL(ctx context.Context, imageURL string, maxResults int) ([]SearchResult, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("无可用图片数据")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, iqdbEndpoint+"?url="+escapeURLParam(imageURL), nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	return c.doSearch(ctx, req, maxResults)
}

// doSearch 发送请求、流式读取响应并解析结果；排队时快速失败。
func (c *iqdbClient) doSearch(ctx context.Context, req *http.Request, maxResults int) ([]SearchResult, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &iqdbRetryableError{msg: "请求失败: " + err.Error(), err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, iqdbStatusError(resp.StatusCode)
	}

	raw, qerr := readIQDBBody(ctx, resp.Body)
	if qerr != nil {
		return nil, qerr
	}

	results := limitResults(parseIQDBResults(raw), maxResults)
	if len(results) == 0 {
		// 响应结束时仍未出结果：可能仍在排队或确无匹配
		if qerr := iqdbQueuedOrNil(raw); qerr != nil {
			return nil, qerr
		}
	}
	return results, nil
}

// queueDetectWindow 排队检测窗口大小（字节）。
// IQDB 排队页在响应头几 KB 内即回传 queue('N','0') 进度标记，
// 无需等待完整响应即可判定仍在排队。
const queueDetectWindow = 16 * 1024

// iqdbQueueFastFailThreshold 排队快速失败的队列位置阈值。
// 实测排队进度约 0.7 位/秒（1335→1313 用时约 30s），因此：
//   - 位置 ≤ 阈值：队列较短，继续读取等待结果（数秒内可完成）
//   - 位置 > 阈值：队列较长（预估等待 >30s），立即中止以免拖累整体回复
const iqdbQueueFastFailThreshold = 20

// readIQDBBody 流式读取响应体；头部检测到长排队（position>阈值）时立即中止。
//
// 实测（2026-08）：高峰期队列位置 1300+ 时，响应头数百 ms 内即包含
// queue('1335','0') 等进度脚本。边读边检测可让长排队请求毫秒级失败，
// 而不是挂满整个超时拖累其它引擎的回复——其他引擎的结果可立即返回，
// 排队由重试或用户稍后重试解决。短排队（位置 ≤ 阈值）继续读取，
// 等待响应自然完成，避免误杀几秒后就能出结果的情况。
//
// 中途断流（连接中断/读取错误）同样包装为可重试错误：IQDB 服务端的
// 队列任务不会因连接断开而取消，但结果已无法送达；重试即重新排队
// （与浏览器刷新后重新提交表单的行为一致），拿到新的队列位置。
func readIQDBBody(ctx context.Context, body io.Reader) ([]byte, error) {
	br := bufio.NewReader(body)
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return nil, &iqdbRetryableError{msg: "读取响应失败: " + ctx.Err().Error(), err: ctx.Err()}
		default:
		}
		n, rerr := br.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			// 仅在窗口内检测：判定长排队不需要大页面，也避免反复正则扫描
			if buf.Len() <= queueDetectWindow {
				if pos, wait, found := iqdbQueuedPosition(buf.Bytes()); found && pos > iqdbQueueFastFailThreshold && wait == 0 {
					return nil, &iqdbQueuedError{position: pos}
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			// 排队中途断流：可重试（重试 = 重新排队，等价浏览器刷新重新提交）
			return nil, &iqdbRetryableError{msg: "读取响应失败: " + rerr.Error(), err: rerr}
		}
	}
	return buf.Bytes(), nil
}

// iqdbQueuedOrNil 检测响应中的排队标记。
//
// IQDB 排队页面在结果区渲染前会不断回传 queue('N','0') 脚本；
// 若响应完成时仍在长队列中（position>阈值 且无结果），返回 iqdbQueuedError，
// 让用户明确知晓排队而非误报"未找到匹配"。position=0 表示正在处理，
// 短排队（≤阈值）则继续等待响应完成，此时若仍无结果按正常无匹配处理。
func iqdbQueuedOrNil(body []byte) error {
	pos, wait, found := iqdbQueuedPosition(body)
	if found && pos > iqdbQueueFastFailThreshold && wait == 0 {
		return &iqdbQueuedError{position: pos}
	}
	return nil
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
