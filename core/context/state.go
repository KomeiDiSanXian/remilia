package context

import (
	"errors"
	"maps"
	"strings"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// state.go — 字符串键扩展状态（ctx.Set / ctx.Get / ctx.All）
//
// 包含：
//   - extensionState 结构定义
//   - isReservedUserStateKey 保留键检查
//   - Set / Get / Delete / All
//   - MustGetString / MustGetInt / GetString / GetInt / GetInt64 / GetBool / GetFloat64

// ErrNilAPI 表示 OpenAPI 未初始化
var ErrNilAPI = errors.New("openAPI is nil")

// ErrNilContext 表示 Context 接收者为 nil
var ErrNilContext = errors.New("context is nil")

// ErrNoPlatformSender 表示 Context 未绑定 platform.Sender（旧路径不支持此操作）
var ErrNoPlatformSender = errors.New("no platform sender: use ReplyGroup/ReplyPrivate for legacy QQ path")

// isReservedUserStateKey reports whether key is reserved for framework internal use.
//
// 注意：此保留键列表仅针对字符串键系统（ctx.Set/ctx.Get）。
// 框架内部通过 ExtSet[T]/ExtGet[T]（类型键系统）存储的数据（如 parsedCommand、
// retryMetadata、middlewareTrace）与字符串键系统完全隔离——
// 即使用户调用 ctx.Set("retry_attempt", v)，也不会覆盖框架存储的 retryMetadata。
// 两套系统使用不同的底层 map，不存在任何键冲突风险。
func isReservedUserStateKey(key string) bool {
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}
	if k == "mw_trace" || k == "retry_attempt" {
		return true
	}
	return strings.HasPrefix(k, "_remilia_internal_")
}

// Set sets a user extensionState value.
//
// # Key-value state system
//
// ctx.Set / ctx.Get 使用字符串键存储 handler 层的临时状态（在同一事件的不同 handler 间传递信息）。
// 这是插件/handler 层推荐的状态存储方式，简单直观。
//
// # nil 值处理
//
// value 为 nil 时，Set 是一个空操作（不删除该键）。
// 若要删除某个键，请显式调用 ctx.Delete(key)。
func (ctx *Context) Set(key string, value any) {
	if ctx == nil {
		return
	}
	if isReservedUserStateKey(key) {
		logger.WithField("key", key).Warn("[Context] set reserved extensionState key is forbidden")
		return
	}
	if value == nil {
		logger.WithField("key", key).Debug("[Context] Set(nil) is a no-op; use ctx.Delete(key) to remove a key")
		return
	}
	s := ExtGetOrInit(ctx.Ext(), func() *extensionState { return newStateExt() })
	s.mu.Lock()
	s.m[key] = value
	s.mu.Unlock()
}

// Delete deletes a user extensionState value stored via ctx.Set.
func (ctx *Context) Delete(key string) {
	if ctx == nil {
		return
	}
	if isReservedUserStateKey(key) {
		logger.WithField("key", key).Warn("[Context] delete reserved extensionState key is forbidden")
		return
	}
	s, ok := ExtGet[*extensionState](ctx.Ext())
	if !ok || s == nil {
		return
	}
	s.mu.Lock()
	delete(s.m, key)
	s.mu.Unlock()
}

// Get gets a user extensionState value.
func (ctx *Context) Get(key string) (any, bool) {
	if ctx == nil {
		return nil, false
	}
	s, ok := ExtGet[*extensionState](ctx.Ext())
	if !ok || s == nil {
		return nil, false
	}
	s.mu.RLock()
	v, ok := s.m[key]
	s.mu.RUnlock()
	return v, ok
}

// All returns a copy of all user extensionState values stored via ctx.Set.
func (ctx *Context) All() map[string]any {
	if ctx == nil {
		return nil
	}
	s, ok := ExtGet[*extensionState](ctx.Ext())
	if !ok || s == nil {
		return map[string]any{}
	}
	s.mu.RLock()
	out := make(map[string]any, len(s.m))
	maps.Copy(out, s.m)
	s.mu.RUnlock()
	return out
}

// MustGetString 获取字符串类型的状态值
func (ctx *Context) MustGetString(key string) (string, error) {
	if val, ok := ctx.Get(key); ok {
		if str, ok := val.(string); ok {
			return str, nil
		}
		return "", errors.New("extensionState key '" + key + "' is not a string")
	}
	return "", errors.New("extensionState key '" + key + "' not found")
}

// MustGetInt 获取整数类型的状态值
func (ctx *Context) MustGetInt(key string) (int, error) {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int); ok {
			return i, nil
		}
		return 0, errors.New("extensionState key '" + key + "' is not an int")
	}
	return 0, errors.New("extensionState key '" + key + "' not found")
}

// GetString 获取字符串类型的状态值
func (ctx *Context) GetString(key string) string {
	if val, ok := ctx.Get(key); ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// GetInt 获取整数类型的状态值
func (ctx *Context) GetInt(key string) int {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int); ok {
			return i
		}
	}
	return 0
}

// GetInt64 获取 int64 类型的状态值
func (ctx *Context) GetInt64(key string) int64 {
	if val, ok := ctx.Get(key); ok {
		if i, ok := val.(int64); ok {
			return i
		}
	}
	return 0
}

// GetBool 获取布尔类型的状态值
func (ctx *Context) GetBool(key string) bool {
	if val, ok := ctx.Get(key); ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

// GetFloat64 获取 float64 类型的状态值
func (ctx *Context) GetFloat64(key string) float64 {
	if val, ok := ctx.Get(key); ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return 0.0
}

// GetUserID 获取用户 ID
func (ctx *Context) GetUserID() string {
	return ctx.GetString("user_id")
}

// SetUserID 设置用户 ID
func (ctx *Context) SetUserID(userID string) {
	ctx.Set("user_id", userID)
}
