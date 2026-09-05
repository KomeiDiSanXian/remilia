package qq

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/tidwall/gjson"
)

// ────────────────────────────────────────────────────────────────────────────
// 合并转发消息（message_type=102）解析
// ────────────────────────────────────────────────────────────────────────────
//
// QQ 官方 API 不提供转发记录回查接口（对比 milky 的 get_forwarded_messages），
// 消息内容是服务端扁平渲染的聊天记录文本。实测存在两种渲染形态：
//
// 形态 A —— 直发的 102 消息（2026-09 报文核验）：首行为 `[记录标题]`，
// 其后按 `=== 消息 N ===` 分块，块内字段行 [消息内容]/[发送者]/[附件N]：
//
//	[月莫法师和蕾米莉亚的聊天记录]
//	=== 消息 1 ===
//	[消息内容] /update now
//	[发送者] 月莫法师
//
// 形态 B —— 被引用的 102（103 引用消息的 msg_elements[0].content，2026-09
// 报文核验）：无标题行，整个记录包在单个 `=== 消息 1 ===` 条目内，条目字段
// 为 `[消息内容] [记录标题]` + `[消息类型]` + `[关联消息]`，记录的实际消息
// 列在 `--- 第N条 ---` 子条目中；条目可再嵌套（嵌套转发/引用逐层 +4 缩进，
// 多行正文以同缩进连续行表达）：
//
//	=== 消息 1 ===
//	[消息内容] [月莫法师和蕾米莉亚的聊天记录]
//	[消息类型] 引用消息
//	[关联消息]
//	--- 第1条 ---
//	    [消息内容] /mc ...
//	    [发送者] 月莫法师
//	--- 第3条 ---
//	    [消息内容] [内层记录标题]
//	    [消息类型] 合并转发消息
//	    [关联消息]
//	    --- 第1条 ---
//	        [消息内容] /update now
//
// 解析结果还原为 ForwardRecord（形态 B 自动解包出内层记录），包装进
// SegmentForward 段 / reply 段 Extra；解析失败时调用方退化为普通文本，
// 保证不丢数据。

// ForwardRecord 合并转发消息的结构化解析结果（平台统一类型别名，
// 定义见 platform.ForwardRecord；下游 AI 插件等经 platform 包消费）。
//
// 存储于 Segment.Extra[qq.ExtraKeyForwardNodes]（直接收到的 102 消息）或
// Segment.Extra[qq.ExtraKeyQuotedForward]（103 引用的消息是 102 合并转发）。
type ForwardRecord = platform.ForwardRecord

// ForwardNode 合并转发记录中的单条子消息（或 [关联消息] 子条目），
// 平台统一类型别名（定义见 platform.ForwardNode）。
type ForwardNode = platform.ForwardNode

// ForwardKind 条目类型（平台统一常量别名，见 platform.ForwardKindQuote /
// platform.ForwardKindRecord）。
const (
	ForwardKindQuote  = platform.ForwardKindQuote
	ForwardKindRecord = platform.ForwardKindRecord
)

// forwardNodeEmpty 报告节点是否不含任何有效内容（用于跳过空块）。
func forwardNodeEmpty(n ForwardNode) bool {
	return len(n.Segments) == 0 && n.Sender.DisplayName == "" && n.Kind == "" && len(n.Related) == 0
}

// forwardKindFromLabel 将渲染文本中的 [消息类型] 标签映射为统一条目类型；
// 未知标签原样保留（平台渲染变体兜底）。
func forwardKindFromLabel(v string) platform.ForwardNodeKind {
	switch v {
	case "引用消息":
		return platform.ForwardKindQuote
	case "合并转发消息":
		return platform.ForwardKindRecord
	default:
		return platform.ForwardNodeKind(v)
	}
}

// ── 行预处理 ────────────────────────────────────────────────────────────────

// fwdLine 是预处理后的单行：前导空格数 + 去缩进文本（去尾随空白）。
type fwdLine struct {
	indent int
	text   string
}

// splitForwardLines 按行拆分渲染文本并计算缩进。
func splitForwardLines(content string) []fwdLine {
	raws := strings.Split(content, "\n")
	lines := make([]fwdLine, 0, len(raws))
	for _, raw := range raws {
		raw = strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(raw, " ")
		lines = append(lines, fwdLine{
			indent: len(raw) - len(trimmed),
			text:   strings.TrimRight(trimmed, " \t"),
		})
	}
	return lines
}

// ── 正则 ────────────────────────────────────────────────────────────────────

var (
	// qqForwardHeaderRe 匹配记录分块头（"=== 消息 1 ==="）。
	qqForwardHeaderRe = regexp.MustCompile(`^===\s*消息\s*\d+\s*===\s*$`)
	// qqForwardSubHeaderRe 匹配 [关联消息] 子条目头（"--- 第1条 ---"）。
	qqForwardSubHeaderRe = regexp.MustCompile(`^---\s*第\s*\d+\s*条\s*---\s*$`)
	// qqForwardTitleRe 匹配记录标题行（"[月莫法师和蕾米莉亚的聊天记录]"）。
	qqForwardTitleRe = regexp.MustCompile(`^\[(.+)\]$`)
	// qqForwardFieldRe 匹配条目字段行。
	qqForwardFieldRe = regexp.MustCompile(`^\[(消息内容|发送者|消息类型|附件\d+|关联消息)\]\s*(.*)$`)
	// qqForwardAttFieldRe 匹配附件行字段键（类型/文件名/尺寸/大小/URL）。
	qqForwardAttFieldRe = regexp.MustCompile(`(类型|文件名|尺寸|大小|URL):`)
	// qqForwardDimsRe 匹配附件尺寸（"600x503"）。
	qqForwardDimsRe = regexp.MustCompile(`^(\d+)x(\d+)$`)
	// qqForwardSizeRe 匹配附件大小（"30.8KB"、"1024B"）。
	qqForwardSizeRe = regexp.MustCompile(`^([\d.]+)\s*(B|KB|MB|GB)$`)
)

// isForwardHeader 报告文本是否为任一种条目头。
func isForwardHeader(text string) bool {
	return qqForwardHeaderRe.MatchString(text) || qqForwardSubHeaderRe.MatchString(text)
}

// ── 递归解析 ────────────────────────────────────────────────────────────────

// fwdParser 是行流上的递归下降解析器。
type fwdParser struct {
	lines []fwdLine
	pos   int
}

// parseForwardRecord 将扁平渲染的聊天记录文本解析为 ForwardRecord。
//
// 严格校验：必须出现至少一个 `=== 消息 N ===` 分块且至少还原出一条非空子消息，
// 普通聊天文本不会误判为记录。
func parseForwardRecord(content string) (*ForwardRecord, bool) {
	p := &fwdParser{lines: splitForwardLines(content)}
	rec := &ForwardRecord{}
	// 标题：首个 === 块之前的 `[...]` 行（形态 B 无标题行，直接命中块头）
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent == 0 && qqForwardHeaderRe.MatchString(ln.text) {
			break
		}
		if rec.Title == "" {
			if m := qqForwardTitleRe.FindStringSubmatch(ln.text); m != nil {
				rec.Title = strings.TrimSpace(m[1])
			}
		}
		p.pos++
	}
	// 块：=== 消息 N ===（缩进 0），块间的空行/杂行跳过
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent != 0 || !qqForwardHeaderRe.MatchString(ln.text) {
			p.pos++
			continue
		}
		if node := p.parseEntry(ln.indent); !forwardNodeEmpty(node) {
			rec.Nodes = append(rec.Nodes, node)
		}
	}
	if len(rec.Nodes) == 0 {
		return nil, false
	}
	return rec, true
}

// parseEntry 解析单个条目（p.pos 处须为 headerIndent 缩进的条目头）。
//
// 条目 = 头行 + 字段行（缩进由首个字段行确定）+ 可选 [关联消息] 子条目树
// （子条目头与字段同缩进，子条目字段逐层 +4）+ 多行正文连续行。
// 在兄弟/上层条目头或反缩进处停止（不消费）。
func (p *fwdParser) parseEntry(headerIndent int) ForwardNode {
	p.pos++ // 消费条目头
	node := ForwardNode{}
	var text strings.Builder
	textStarted := false
	fieldIndent := -1
	sawRelated := false
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		// [关联消息] 之后的 --- 子条目：递归收集（须先于条目头判断，
		// 否则与父条目同缩进的子条目头会被误判为兄弟）
		if sawRelated && qqForwardSubHeaderRe.MatchString(ln.text) &&
			(fieldIndent == -1 || ln.indent >= fieldIndent) {
			node.Related = p.parseSubEntries(ln.indent)
			sawRelated = false // [关联消息] 至多一个，其后同缩进头属兄弟层级
			continue
		}
		if isForwardHeader(ln.text) && ln.indent <= headerIndent {
			break // 下一个兄弟/上层条目
		}
		if fieldIndent == -1 {
			fieldIndent = ln.indent
		}
		if m := qqForwardFieldRe.FindStringSubmatch(ln.text); m != nil && ln.indent == fieldIndent {
			switch m[1] {
			case "消息内容":
				textStarted = true
				if v := strings.TrimSpace(m[2]); v != "" {
					text.WriteString(v)
					text.WriteString("\n")
				}
			case "发送者":
				node.Sender = platform.UserInfo{DisplayName: strings.TrimSpace(m[2])}
			case "消息类型":
				node.Kind = forwardKindFromLabel(strings.TrimSpace(m[2]))
			case "关联消息":
				sawRelated = true
			default: // 附件N
				if att, ok := parseForwardAttachment(m[2]); ok {
					node.Segments = append(node.Segments,
						platform.Segment{Type: mediaSegmentType(att), Attachment: att})
				}
			}
			p.pos++
			continue
		}
		if ln.indent < fieldIndent {
			break // 反缩进：属于上层条目
		}
		// 连续行：并入正文（多行消息内容）；空行仅在正文已开始时保留
		if !isForwardHeader(ln.text) {
			if ln.text == "" {
				if textStarted {
					text.WriteString("\n")
				}
			} else {
				textStarted = true
				text.WriteString(ln.text)
				text.WriteString("\n")
			}
		}
		p.pos++
	}
	if t := strings.TrimSpace(text.String()); t != "" {
		// 正文在字段解析期后置收集，统一插到媒体段之前
		node.Segments = append([]platform.Segment{{Type: platform.SegmentText, Text: t}}, node.Segments...)
	}
	return node
}

// parseSubEntries 解析 indent 缩进处的连续 --- 子条目（[关联消息] 内容）。
func (p *fwdParser) parseSubEntries(indent int) []ForwardNode {
	var out []ForwardNode
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent != indent || !qqForwardSubHeaderRe.MatchString(ln.text) {
			break
		}
		if node := p.parseEntry(indent); !forwardNodeEmpty(node) {
			out = append(out, node)
		}
	}
	return out
}

// normalizeForwardRecord 识别形态 B（被引用记录的包裹渲染）并解包：
// 单块单条目、条目带 [消息类型] 标记、无发送者、仅一段括号标题文本且携带
// [关联消息] 时，该条目即记录摘要，Related 才是记录的实际消息。
func normalizeForwardRecord(rec *ForwardRecord) *ForwardRecord {
	if len(rec.Nodes) != 1 {
		return rec
	}
	n := rec.Nodes[0]
	if n.Kind == "" || len(n.Related) == 0 || n.Sender.DisplayName != "" || len(n.Segments) != 1 ||
		n.Segments[0].Type != platform.SegmentText {
		return rec
	}
	inner := n.Segments[0].Text
	if m := qqForwardTitleRe.FindStringSubmatch(inner); m != nil {
		inner = strings.TrimSpace(m[1])
	}
	return &ForwardRecord{Title: inner, Nodes: n.Related}
}

// ── 段包装与提取入口 ────────────────────────────────────────────────────────

// forwardSegment 将合并转发扁平文本解析为 SegmentForward 段。
//
// Extra 携带：
//   - ExtraKeyForwardNodes：*ForwardRecord（结构化子消息）
//   - platform.SegmentExtraTitle / SegmentExtraSummary：跨平台降级摘要
//
// 解析失败返回 ok=false，调用方退化为普通文本。
func forwardSegment(content string) (platform.Segment, bool) {
	rec, ok := parseForwardRecord(content)
	if !ok {
		return platform.Segment{}, false
	}
	return forwardRecordSegment(normalizeForwardRecord(rec)), true
}

// forwardRecordSegment 将解析结果包装为 SegmentForward 段。
func forwardRecordSegment(rec *ForwardRecord) platform.Segment {
	return platform.Segment{
		Type: platform.SegmentForward,
		Extra: map[string]any{
			ExtraKeyForwardNodes:         rec,
			platform.SegmentExtraTitle:   rec.Title,
			platform.SegmentExtraSummary: forwardSummary(rec),
		},
	}
}

// quotedForwardRecord 从 103 引用消息的 msg_elements / parallel_message 中
// 提取被引用的合并转发记录（被引用消息为 102）。
//
// 取数顺序：优先 msg_elements[0]（与 103 纯文本引用的被引用消息位置一致，
// 2026-09 实测该元素可能不携带 message_type 字段），兜底扫
// parallel_message.msg_nodes 中的 102 节点。未标 message_type=102 时，内容
// 仍满足严格记录格式（有标题或形态 B 包裹结构）则同样接受，防止官方渲染
// 变体漏解析。
func quotedForwardRecord(elements, parallel gjson.Result) *ForwardRecord {
	if elements.IsArray() {
		if arr := elements.Array(); len(arr) > 0 {
			if rec := quotedRecordFromText(arr[0].Get("content").String(),
				arr[0].Get("message_type").Int() == 102); rec != nil {
				return rec
			}
		}
	}
	if nodes := parallel.Get("msg_nodes"); nodes.IsArray() {
		for _, n := range nodes.Array() {
			if rec := quotedRecordFromText(n.Get("content").String(),
				n.Get("message_type").Int() == 102); rec != nil {
				return rec
			}
		}
	}
	return nil
}

// quotedRecordFromText 从被引用元素的 content 提取记录；marked 表示元素显式
// 标记 message_type=102。无标记时要求解析结果带标题，防普通文本误判。
func quotedRecordFromText(content string, marked bool) *ForwardRecord {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	rec, ok := parseForwardRecord(content)
	if !ok {
		return nil
	}
	rec = normalizeForwardRecord(rec)
	if !marked && rec.Title == "" {
		return nil
	}
	return rec
}

// ── 摘要 ────────────────────────────────────────────────────────────────────

// forwardSummary 生成转发记录摘要（跨平台降级用）：前 3 条子消息的单行预览，
// 超出时附总条数。
func forwardSummary(rec *ForwardRecord) string {
	const maxPreview = 3
	var lines []string
	for i, n := range rec.Nodes {
		if i == maxPreview {
			lines = append(lines, fmt.Sprintf("… 共 %d 条消息", len(rec.Nodes)))
			break
		}
		if p := forwardPreview(n); p != "" {
			lines = append(lines, p)
		}
	}
	return strings.Join(lines, "\n")
}

// forwardPreview 生成子消息的单行预览：发送者 + 首个内容段摘要。
func forwardPreview(n ForwardNode) string {
	for _, s := range n.Segments {
		var desc string
		switch s.Type {
		case platform.SegmentText:
			if t := strings.TrimSpace(s.Text); t != "" {
				desc = truncateForwardText(t, 32)
			}
		case platform.SegmentImage:
			desc = "[图片]"
		case platform.SegmentAudio:
			desc = "[语音]"
		case platform.SegmentVideo:
			desc = "[视频]"
		case platform.SegmentFile:
			desc = "[文件]"
		default:
			continue
		}
		if desc == "" {
			continue
		}
		return forwardPreviewJoin(n, desc)
	}
	if n.Kind != "" && len(n.Related) > 0 {
		return forwardPreviewJoin(n, forwardKindLabel(n.Kind))
	}
	return n.Sender.DisplayName
}

// forwardKindLabel 将统一条目类型映射为降级摘要中的可读标记。
func forwardKindLabel(k platform.ForwardNodeKind) string {
	switch k {
	case platform.ForwardKindQuote:
		return "[引用消息]"
	case platform.ForwardKindRecord:
		return "[嵌套聊天记录]"
	default:
		return "[" + string(k) + "]"
	}
}

// forwardPreviewJoin 以发送者昵称为前缀拼接预览片段。
func forwardPreviewJoin(n ForwardNode, desc string) string {
	if n.Sender.DisplayName == "" {
		return desc
	}
	return n.Sender.DisplayName + ": " + desc
}

// truncateForwardText 按 rune 截断文本，超长补省略号。
func truncateForwardText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// ── 附件行字段解析 ──────────────────────────────────────────────────────────

// parseForwardAttachment 解析 [附件N] 行字段串（URL: 后的部分）。
//
// 字段以已知键定位切片（文件名可含空格，不能按空白分词）：
//
//	类型:图片 文件名:AA.png 尺寸:600x503 大小:30.8KB URL:https://...
func parseForwardAttachment(rest string) (platform.Attachment, bool) {
	var att platform.Attachment
	locs := qqForwardAttFieldRe.FindAllStringSubmatchIndex(rest, -1)
	if len(locs) == 0 {
		return platform.Attachment{}, false
	}
	for i, loc := range locs {
		key := rest[loc[2]:loc[3]]
		end := len(rest)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		val := strings.TrimSpace(rest[loc[1]:end])
		switch key {
		case "类型":
			att.Kind = forwardAttachmentKind(val)
		case "文件名":
			att.Name = val
		case "尺寸":
			if m := qqForwardDimsRe.FindStringSubmatch(val); m != nil {
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])
				att.Width, att.Height = w, h
			}
		case "大小":
			att.Size = parseForwardSize(val)
		case "URL":
			att.URL = val
		}
	}
	if att.URL == "" && att.Name == "" {
		return platform.Attachment{}, false
	}
	return att, true
}

// forwardAttachmentKind 将附件行"类型"值映射为统一附件类别。
func forwardAttachmentKind(v string) platform.AttachmentKind {
	switch v {
	case "图片":
		return platform.AttachmentKindImage
	case "视频":
		return platform.AttachmentKindVideo
	case "语音", "音频":
		return platform.AttachmentKindAudio
	default:
		return platform.AttachmentKindFile
	}
}

// parseForwardSize 将附件大小字符串换算为近似字节数（"30.8KB" → 31539）。
func parseForwardSize(v string) int {
	m := qqForwardSizeRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(v)))
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch m[2] {
	case "KB":
		f *= 1024
	case "MB":
		f *= 1024 * 1024
	case "GB":
		f *= 1024 * 1024 * 1024
	}
	return int(f)
}
