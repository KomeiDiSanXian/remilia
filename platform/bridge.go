// bridge.go — 旧版 Adapter 桥接层（过渡期保留）
//
// Deprecated: P2 引擎迁移完成后本文件将被删除。
// 请使用 PlatformAdapter 接口替代 LegacyAdapter。
package platform

import (
	stdctx "context"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// LegacyEventHandler 是旧版事件处理函数的签名（与 engine.Adapter 兼容）
//
// Deprecated: 使用 func(Event) 替代。
type LegacyEventHandler func(*dto.Payload)

// LegacyAdapter 是旧版适配器接口（与 engine.Adapter 完全相同）
//
// Deprecated: 使用 PlatformAdapter 替代。
// 待所有 QQ 适配器迁移到 PlatformAdapter 后，本接口将被删除。
type LegacyAdapter interface {
	Start(ctx stdctx.Context, handleFunc func(*dto.Payload)) error
	Stop(ctx stdctx.Context) error
}
