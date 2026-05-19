package qq

import "github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"

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
