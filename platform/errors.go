package platform

import "fmt"

// ────────────────────────────────────────────────────────────────────────────
// SendErrorCode
// ────────────────────────────────────────────────────────────────────────────

// SendErrorCode 是 [SendError] 的标准错误码，标识消息发送失败的具体原因。
//
// 各平台 Sender 实现应将平台特定错误映射到此枚举，
// 让调用方无需解析错误字符串即可做针对性处理。
type SendErrorCode int

const (
	// SendErrUnknown 未知/未分类错误（default，不应主动使用）
	SendErrUnknown SendErrorCode = iota

	// SendErrRateLimit 触发平台频率限制（429 / too many requests）
	//
	// 建议：退避重试，时间间隔由 SendError.RetryAfter 指示（0 表示未知）。
	SendErrRateLimit

	// SendErrPermDenied 权限不足（机器人无发言权限、被屏蔽、未加入目标会话等）
	SendErrPermDenied

	// SendErrMsgTooLong 消息内容超出平台字符/字节长度限制
	SendErrMsgTooLong

	// SendErrUnsupported 当前平台不支持该消息类型（如向不支持 Embed 的平台发送 Embed）
	SendErrUnsupported

	// SendErrInvalidTarget 目标会话无效（ID 不存在、已解散、机器人未在其中）
	SendErrInvalidTarget

	// SendErrNetworkError 网络/连接错误（超时、DNS 失败、连接被重置等）
	//
	// 此类错误通常可重试。
	SendErrNetworkError

	// SendErrTokenExpired 平台授权令牌已过期（被动回复 token 超时、access_token 失效等）
	SendErrTokenExpired

	// SendErrDuplicate 消息重复（平台防重放拒绝，msg_seq 重复等）
	SendErrDuplicate

	// SendErrPlatform 平台返回的其他明确错误（已有 Code 和 Message，但不属于以上类别）
	SendErrPlatform
)

// String 返回错误码的可读名称，便于日志输出。
func (c SendErrorCode) String() string {
	switch c {
	case SendErrRateLimit:
		return "RATE_LIMIT"
	case SendErrPermDenied:
		return "PERM_DENIED"
	case SendErrMsgTooLong:
		return "MSG_TOO_LONG"
	case SendErrUnsupported:
		return "UNSUPPORTED"
	case SendErrInvalidTarget:
		return "INVALID_TARGET"
	case SendErrNetworkError:
		return "NETWORK_ERROR"
	case SendErrTokenExpired:
		return "TOKEN_EXPIRED"
	case SendErrDuplicate:
		return "DUPLICATE"
	case SendErrPlatform:
		return "PLATFORM_ERROR"
	default:
		return "UNKNOWN"
	}
}

// Retryable 返回此错误码是否通常可重试。
//
// 使用示例：
//
//	if se, ok := platform.AsSendError(err); ok && se.Code.Retryable() {
//	    time.Sleep(se.RetryAfter)
//	    return sender.Send(ctx, req)
//	}
func (c SendErrorCode) Retryable() bool {
	switch c {
	case SendErrRateLimit, SendErrNetworkError, SendErrTokenExpired:
		return true
	default:
		return false
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SendError
// ────────────────────────────────────────────────────────────────────────────

// SendError 是 [Sender.Send] 返回的结构化错误。
//
// 平台适配器实现应在以下情况包装为 SendError：
//   - 触发平台频率限制时用 [SendErrRateLimit]
//   - 权限不足时用 [SendErrPermDenied]
//   - 消息过长时用 [SendErrMsgTooLong]
//   - 其他明确平台错误用 [SendErrPlatform]
//
// 使用 [AsSendError] 安全提取，无需直接类型断言：
//
//	if se, ok := platform.AsSendError(err); ok {
//	    switch se.Code {
//	    case platform.SendErrRateLimit:
//	        time.Sleep(se.RetryAfter)
//	    case platform.SendErrPermDenied:
//	        log.Warn("bot lacks send permission in", se.ChatID)
//	    }
//	}
type SendError struct {
	// Code 标准错误码
	Code SendErrorCode
	// Platform 来源平台（如 "qq"、"discord"）
	Platform string
	// ChatID 目标会话 ID（便于日志定位）
	ChatID string
	// Message 人可读的错误描述（可含平台原始错误信息）
	Message string
	// RetryAfter 建议重试等待时间（仅 SendErrRateLimit 有意义，0 表示未知）
	RetryAfter int // 秒
	// Cause 底层原始错误（平台 SDK 返回的 error）
	Cause error
}

// Error 实现 error 接口。
func (e *SendError) Error() string {
	if e.ChatID != "" {
		return fmt.Sprintf("platform %s send error [%s] to %s: %s", e.Platform, e.Code, e.ChatID, e.Message)
	}
	return fmt.Sprintf("platform %s send error [%s]: %s", e.Platform, e.Code, e.Message)
}

// Unwrap 支持 errors.Is / errors.As 的错误链穿透。
func (e *SendError) Unwrap() error { return e.Cause }

// AsSendError 从任意 error 链中提取 *SendError。
//
// 若 err 或其链上存在 *SendError，返回 (se, true)；否则返回 (nil, false)。
// 推荐使用此函数代替直接类型断言，支持 %w 包装的多层错误链。
//
// 使用示例：
//
//	if se, ok := platform.AsSendError(err); ok && se.Code == platform.SendErrRateLimit {
//	    time.Sleep(time.Duration(se.RetryAfter) * time.Second)
//	}
func AsSendError(err error) (*SendError, bool) {
	var se *SendError
	if asErr(err, &se) {
		return se, true
	}
	return nil, false
}

// IsSendErrorCode 判断 err 链中是否存在指定 SendErrorCode。
//
// 等价于 AsSendError(err) && se.Code == code，但更简洁。
//
// 使用示例：
//
//	if platform.IsSendErrorCode(err, platform.SendErrPermDenied) {
//	    notifyAdmin("bot lacks permission")
//	}
func IsSendErrorCode(err error, code SendErrorCode) bool {
	se, ok := AsSendError(err)
	return ok && se.Code == code
}

// NewSendError 构造一个 SendError（最常用的快速构造函数）。
//
// 平台适配器在 Sender.Send() 实现中使用：
//
//	return platform.NewSendError(platform.SendErrRateLimit, "qq", chatID,
//	    "触发频率限制", 5, apiErr)
func NewSendError(code SendErrorCode, plt, chatID, msg string, retryAfter int, cause error) *SendError {
	return &SendError{
		Code:       code,
		Platform:   plt,
		ChatID:     chatID,
		Message:    msg,
		RetryAfter: retryAfter,
		Cause:      cause,
	}
}

// asErr 是对 errors.As 的薄包装，避免在本包直接 import "errors" 产生循环依赖风险。
// （实际上 platform 包不依赖 errors 循环，但保持单一 import 入口便于追踪）
func asErr(err error, target **SendError) bool {
	// 手动实现 errors.As 的核心逻辑（无需额外 import）
	for err != nil {
		if se, ok := err.(*SendError); ok {
			*target = se
			return true
		}
		type unwrapper interface{ Unwrap() error }
		type multiUnwrapper interface{ Unwrap() []error }
		switch u := err.(type) {
		case multiUnwrapper:
			for _, e := range u.Unwrap() {
				if asErr(e, target) {
					return true
				}
			}
			return false
		case unwrapper:
			err = u.Unwrap()
		default:
			return false
		}
	}
	return false
}
