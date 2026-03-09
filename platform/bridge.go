// bridge.go — 将 PlatformAdapter 桥接到旧版 engine.Adapter 接口
//
// 过渡期设计：
//   - LegacyBridge 让新平台适配器无缝接入现有 Engine，无需修改 Engine 内部
//   - 旧 QQ 适配器（webhookAdapter）仍可直接使用，零迁移成本
//   - 待 Engine 彻底解耦后，本文件可直接删除
package platform

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// LegacyEventHandler 是旧版事件处理函数的签名（与 engine.Adapter 兼容）
type LegacyEventHandler func(*dto.Payload)

// LegacyAdapter 是旧版适配器接口（与 engine.Adapter 完全相同）
//
// 定义在此处是为了让 platform 包能独立使用 bridge，
// 而不在此引入 engine 包（避免循环依赖）。
type LegacyAdapter interface {
	Start(ctx stdctx.Context, handleFunc func(*dto.Payload)) error
	Stop(ctx stdctx.Context) error
}
