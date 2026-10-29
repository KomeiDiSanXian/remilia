package qq

import (
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// ArkKVField 是 ARK 模板中 obj_kv 内的单个键值对。
type ArkKVField struct {
	Key   string
	Value string
}

// ArkObj 是 ARK 模板中数组类型 KV 的一个对象元素。
type ArkObj struct {
	KV []ArkKVField
}

// ArkKV 是 ARK 模板的一个 KV 键值对。
//
// 支持两种类型：
//   - 简单字符串值：设置 Value
//   - 数组对象值：设置 Obj（如 #LIST# 模板变量）
type ArkKV struct {
	Key   string
	Value string
	Obj   []ArkObj
}

// Ark 是 QQ 平台 ARK 模板消息。
//
// 通过 qq.ApplyExtra 注入到 platform.OutboundMessage。
// 目前系统内置三个可用模板 ID：23（链接+文本列表）、24（文本+缩略图）、37（大图）。
type Ark struct {
	TemplateID int
	KV         []ArkKV
}

// convertArk 将 Ark 转换为 dto.Ark。
//
// 简单 KV 映射为 {"key": "...", "value": "..."}
// 数组 KV（Obj 非空）映射为 {"key": "...", "obj": [{"obj_kv": [{"key": "...", "value": "..."}]}]}
func convertArk(ark *Ark) *dto.Ark {
	if ark == nil {
		return nil
	}
	dtoKV := make([]map[string]any, 0, len(ark.KV))
	for _, kv := range ark.KV {
		entry := map[string]any{"key": kv.Key}
		if len(kv.Obj) > 0 {
			objList := make([]map[string]any, 0, len(kv.Obj))
			for _, obj := range kv.Obj {
				objKV := make([]map[string]string, 0, len(obj.KV))
				for _, field := range obj.KV {
					objKV = append(objKV, map[string]string{"key": field.Key, "value": field.Value})
				}
				objList = append(objList, map[string]any{"obj_kv": objKV})
			}
			entry["obj"] = objList
		} else {
			entry["value"] = kv.Value
		}
		dtoKV = append(dtoKV, entry)
	}
	return &dto.Ark{
		TemplateID: ark.TemplateID,
		KV:         dtoKV,
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 入站结构化卡片（message_type=3）解析
// ────────────────────────────────────────────────────────────────────────────

// 结构化卡片的 ark_type 取值（2026-09-16 官方文档新增 picture）。
const (
	// ArkTypeTuwen 图文 H5（如快手等外部 App 的分享链接）。
	ArkTypeTuwen = "tuwen"
	// ArkTypeFeed 图文卡片（群相册、频道帖子、分享卡片）。
	ArkTypeFeed = "feed"
	// ArkTypeMiniApp 小程序（微信/QQ 小程序、哔哩哔哩等）。
	ArkTypeMiniApp = "miniapp"
	// ArkTypeMap 位置卡片。
	ArkTypeMap = "map"
	// ArkTypeContactCard 好友名片。
	ArkTypeContactCard = "contact_card"
	// ArkTypeVideoShare 视频分享。
	ArkTypeVideoShare = "video_share"
	// ArkTypeMusicTogether 一起听歌。
	ArkTypeMusicTogether = "music_together"
	// ArkTypePicture 纯图片卡片（2026-09-16 新增的枚举值）。
	ArkTypePicture = "picture"
)

// ArkData 是入站结构化卡片（message_type=3）解析后的视图。
//
// 原始 JSON 仍保留在 Segment.Extra[qq.ExtraKeyArkData] 里（字段远多于官方文档），
// 这里只把各 ark_type 通用的部分抽成字段，便于业务代码直接取值。
//
// 用法：
//
//	for _, att := range platform.Attachments(ctx.GetEvent()) { ... } // 不含卡片
//	for _, seg := range ctx.GetSegments() {
//	    if data, ok := qq.ArkDataFromSegment(seg); ok {
//	        switch data.Type {
//	        case qq.ArkTypePicture:
//	            imgURL := data.Fields["preview"]
//	        }
//	    }
//	}
type ArkData struct {
	// Type 卡片类型标识（见 ArkType* 常量）。
	Type string
	// Name 卡片类型的中文名称，如"图文 H5"、"小程序"。
	Name string
	// Prompt 卡片上的用户操作提示文本。
	Prompt string
	// Fields 卡片字段原始映射，常见键名：tag/tags=来源标签、title=标题、
	// desc=描述、jump_url=跳转链接、preview=预览图、source=来源名称、
	// source_logo=来源图标、tag_icon=标签图标、nickname=昵称、avatar=头像、
	// address=地址。
	Fields map[string]string
}

// ArkDataFromSegment 从结构化卡片段中解析 ArkData。
//
// 段由 event.go 的 arkDataSegment 产出：Type=SegmentUnknown，
// Extra["type"]=="ark_data"，原始 JSON 在 Extra[ExtraKeyArkData]。
// 非卡片段返回 (零值, false)。
func ArkDataFromSegment(seg platform.Segment) (ArkData, bool) {
	raw, ok := seg.Extra[ExtraKeyArkData].(string)
	if !ok || raw == "" {
		return ArkData{}, false
	}
	return ParseArkData(raw)
}

// ParseArkData 解析 ark_data 原始 JSON。
//
// fields 的取值类型不固定（多数是字符串，个别是数组），这里统一按字符串收集：
// 数组取逗号拼接，对象取原始 JSON，缺失键不写入 map。
// 非 JSON 对象（空串、语法错误、null、数组等）返回 (零值, false)。
func ParseArkData(raw string) (ArkData, bool) {
	if raw == "" || !gjson.Valid(raw) {
		return ArkData{}, false
	}
	res := gjson.Parse(raw)
	if !res.IsObject() {
		return ArkData{}, false
	}
	data := ArkData{
		Type:   res.Get("ark_type").String(),
		Name:   res.Get("ark_name").String(),
		Prompt: res.Get("prompt").String(),
	}
	if fields := res.Get("fields"); fields.IsObject() {
		data.Fields = make(map[string]string, len(fields.Map()))
		fields.ForEach(func(k, v gjson.Result) bool {
			switch {
			case v.IsArray():
				parts := make([]string, 0, len(v.Array()))
				for _, item := range v.Array() {
					parts = append(parts, item.String())
				}
				data.Fields[k.String()] = joinNonEmpty(parts, ",")
			case v.IsObject():
				data.Fields[k.String()] = v.Raw
			default:
				data.Fields[k.String()] = v.String()
			}
			return true
		})
	}
	return data, true
}

// joinNonEmpty 用 sep 连接非空元素（空切片返回空串而不是分隔符）。
func joinNonEmpty(parts []string, sep string) string {
	var out string
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	return out
}
