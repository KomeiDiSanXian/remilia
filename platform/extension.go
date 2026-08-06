package platform

// extension.go — 平台扩展能力访问机制
//
// 平台能力通过**双通道**暴露，各司其职、互不影响：
//
//	通道 A（平台无关）                通道 B（平台特有）
//	─────────────────────            ─────────────────────
//	platform.Sender                  platform.APIProvider
//	+ 15 个可选接口                   （本文件）
//	（Send / GetDeleter /            → 平台特有 API 句柄：
//	  GetGroupManager / ...）           *onebot.Sender、
//	                                   openapi.OpenAPI、
//	用法：                             *milky.Adapter 等
//	  sender := ctx.GetPlatformSender()
//	                                 用法：
//	                                  api := platform.GetPlatformAPIAs[
//	                                    *onebot.Sender](sender)
//
// 规则：
//   - **平台无关操作永远从通道 A 走**（Send、撤回、群管理、历史消息、
//     公告等均可选接口或辅助函数完成），不受通道 B 影响；
//   - 通道 B 仅用于访问平台特有 API（如 OneBot 扩展动作、QQ 频道管理、
//     Milky 群管理动作），这些能力不存在跨平台语义；
//   - 拿到通道 B 的句柄后，若还需要平台无关操作，回到通道 A 即可
//     （事件处理器中 ctx.GetPlatformSender() 始终可用）。

// APIProvider 由平台 Sender 实现，向调用方暴露平台特有的 API 句柄。
//
// 返回的句柄是**操作面**：请仅调用其方法，勿保存句柄跨请求复用，
// 也勿尝试构造零值句柄（内部依赖未初始化时会 panic）。
//
// PlatformAPI 的返回值类型与字段可见性（各平台包文档说明）：
//
//	platform/onebot   → *onebot.Sender        字段私有，仅可调用方法
//	platform/qq       → openapi.OpenAPI      接口，无字段
//	platform/milky    → *milky.Adapter       字段私有，仅可调用方法
//	platform/satori   → *satori.Client       字段私有，仅可调用方法
//	platform/discord  → *discordgo.Session   第三方 SDK，**字段公开**，
//	                                          只读使用其方法，勿修改内部状态
//	platform/telegram → *telegram.Client     字段私有，仅可调用方法
//
// 实现方为各平台的 sender 类型（onebot.Sender 为公开类型，其余为包内
// 私有类型）；调用方无需关心实现类型，通过 [GetPlatformAPIAs] 断言
// 上表列出的公开句柄类型即可。
//
// 使用 [GetPlatformAPIAs]（推荐）或 [GetPlatformAPI] 直接断言。
type APIProvider interface {
	// PlatformAPI 返回平台特有的 API 句柄。
	PlatformAPI() any
}

// GetPlatformAPIAs 泛型获取平台特有 API 句柄。
//
// 从 Sender 提取 APIProvider 句柄并断言为目标类型 T（具体类型或接口均可），
// 避免 [GetPlatformAPI] 返回 any 后的手工断言。**仅用于平台特有 API**；
// 平台无关操作请直接使用 Sender 接口本身：
//
//	api, ok := platform.GetPlatformAPIAs[*onebot.Sender](ctx.GetPlatformSender())
//	if ok {
//	    _, _ = api.SendGroupForwardMsg(ctx, 123456, nodes)   // 平台特有
//	}
//
//	// 平台无关操作仍走 Sender（二者互补，互不影响）
//	if gm, ok := platform.GetGroupManager(adapter); ok {
//	    _ = gm.BanMember(ctx, groupID, userID, 60)
//	}
//
//	// T 也可以是接口（如 QQ 的 OpenAPI 接口）
//	api, ok := platform.GetPlatformAPIAs[openapi.OpenAPI](sender)
//
// 若 Sender 未实现 APIProvider 或句柄类型不是 T，返回 (零值, false)。
func GetPlatformAPIAs[T any](sender Sender) (T, bool) {
	var zero T
	api := GetPlatformAPI(sender)
	if api == nil {
		return zero, false
	}
	t, ok := api.(T)
	if !ok {
		return zero, false
	}
	return t, true
}

// GetPlatformAPI 从 platform.Sender 提取平台特有 API 句柄。
//
// 返回 any，调用方需自行断言目标类型；推荐优先使用泛型版本
// [GetPlatformAPIAs]，类型安全且无需断言：
//
//	api := platform.GetPlatformAPI(ctx.GetPlatformSender())
//	if one, ok := api.(*onebot.Sender); ok {
//	    _ = one.SendGroupSign(ctx, 123456)
//	}
func GetPlatformAPI(sender Sender) any {
	if p, ok := sender.(APIProvider); ok {
		return p.PlatformAPI()
	}
	return nil
}
