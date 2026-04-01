// Package verifycode 提供独立的验证码插件。
//
// 这是从 admin 插件中拆分出的独立验证码插件，可单独注册使用，
// 也可与 admin 插件一起使用（admin 插件会自动识别并使用本插件）。
//
// 功能：
//   - 生成验证码（绑定角色/权限）
//   - 验证码验证（支持一次性和多次使用）
//   - 有效期控制
//   - 验证码吊销
//
// 使用示例:
//
//	pm.Register(verifycode.New())
//	vc := ctx.MustGet("verifycode").(*verifycode.Plugin)
//	code, _ := vc.Generate(verifycode.Config{Role: "vip", TTL: 24*time.Hour, MaxUses: 1})
//	role, err := vc.Verify(userID, codeStr)
package verifycode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	storage "github.com/KomeiDiSanXian/remilia/builtin/core/storage"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// CodeConfig 验证码配置
type CodeConfig struct {
	// Role 验证成功后授予的角色
	Role string
	// TTL 有效期（0 表示永不过期）
	TTL time.Duration
	// MaxUses 最大使用次数（0=一次性，<0=无限次）
	MaxUses int
}

// CodeEntry 验证码记录
type CodeEntry struct {
	Code      string     `json:"code"`
	Role      string     `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	MaxUses   int        `json:"max_uses"`
	UsedCount int        `json:"used_count"`
	UsedBy    []string   `json:"used_by,omitempty"`
}

// IsExpired 检查验证码是否已过期
func (e *CodeEntry) IsExpired() bool {
	return e.ExpiresAt != nil && time.Now().After(*e.ExpiresAt)
}

// IsExhausted 检查验证码是否已达使用上限
func (e *CodeEntry) IsExhausted() bool {
	return e.MaxUses == 0 && e.UsedCount >= 1 // 一次性
}

// IsValid 检查验证码是否可用
func (e *CodeEntry) IsValid() bool {
	if e.IsExpired() {
		return false
	}
	if e.MaxUses == 0 && e.UsedCount >= 1 {
		return false // 一次性已使用
	}
	if e.MaxUses > 0 && e.UsedCount >= e.MaxUses {
		return false // 达到使用上限
	}
	return true
}

// OnVerifyHook 验证成功后的回调（用于授予角色/权限）
type OnVerifyHook func(userID, role string) error

// storageBackend 接口已合并至 storage.Client，见 plugins/core/storage

// Plugin 验证码插件
type Plugin struct {
	mu       sync.RWMutex
	codes    map[string]*CodeEntry // code -> entry
	onVerify OnVerifyHook
	store    *storage.Store // 可选持久化后端（nil=纯内存）
}

// NewPlugin 创建 Plugin 实例
func NewPlugin(onVerify OnVerifyHook) *Plugin {
	return &Plugin{
		codes:    make(map[string]*CodeEntry),
		onVerify: onVerify,
	}
}

// New 创建验证码插件描述符
func New(onVerify OnVerifyHook) *plugin.Descriptor {
	p := NewPlugin(onVerify)
	return Descriptor(p)
}

// Descriptor 从已有 Plugin 创建描述符
func Descriptor(p *Plugin) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:         "verifycode",
		Version:      "1.0.0",
		Deps:         []string{},
		OptionalDeps: []string{"storage"},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "多用途验证码插件，支持角色授予、入群验证、自定义回调",
			Category:    "核心",
			Tags:        []string{"验证码", "安全", "授权"},
			HelpText: `验证码插件使用说明：
  vc := plugin.Require[verifycode.Plugin](ctx, "verifycode")
  code, _ := vc.Generate(verifycode.CodeConfig{Role: "vip", TTL: 24*time.Hour})
  role, err := vc.Verify(userID, code)`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Info("Plugin loaded")
			if sb, ok := plugin.Try[storage.Plugin](ctx, "storage"); ok {
				p.store = sb.NS("verifycode")
				p.load()
			}
			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.API.(*Plugin).save()
			return nil
		},
	}
}

// Generate 生成验证码，返回验证码字符串
func (p *Plugin) Generate(cfg CodeConfig) (string, error) {
	if cfg.Role == "" {
		return "", fmt.Errorf("role is required")
	}

	code, err := generateCode()
	if err != nil {
		return "", fmt.Errorf("failed to generate code: %w", err)
	}

	entry := &CodeEntry{
		Code:      code,
		Role:      cfg.Role,
		CreatedAt: time.Now(),
		MaxUses:   cfg.MaxUses,
	}

	if cfg.TTL > 0 {
		exp := time.Now().Add(cfg.TTL)
		entry.ExpiresAt = &exp
	}

	p.mu.Lock()
	p.codes[code] = entry
	p.mu.Unlock()

	go p.save()
	logger.Infof("[VerifyCode] Generated code for role %s (ttl=%v maxUses=%d)", cfg.Role, cfg.TTL, cfg.MaxUses)
	return code, nil
}

// Verify 验证验证码，成功则调用 OnVerifyHook 并返回角色名
func (p *Plugin) Verify(userID, code string) (string, error) {
	p.mu.Lock()
	entry, exists := p.codes[code]
	if !exists {
		p.mu.Unlock()
		return "", fmt.Errorf("invalid or expired code")
	}
	if !entry.IsValid() {
		p.mu.Unlock()
		return "", fmt.Errorf("code is expired or exhausted")
	}

	// 检查是否已被该用户使用
	if slices.Contains(entry.UsedBy, userID) {
		p.mu.Unlock()
		return "", fmt.Errorf("code already used by this user")
	}

	role := entry.Role
	entry.UsedCount++
	entry.UsedBy = append(entry.UsedBy, userID)

	// 如果一次性验证码，移除
	if entry.MaxUses == 0 {
		delete(p.codes, code)
	}
	p.mu.Unlock()

	// 调用授权回调
	if p.onVerify != nil {
		if err := p.onVerify(userID, role); err != nil {
			return "", fmt.Errorf("failed to grant role: %w", err)
		}
	}

	go p.save()
	logger.Infof("[VerifyCode] User %s verified with code %s, granted role %s", userID, code, role)
	return role, nil
}

// Revoke 吊销验证码
func (p *Plugin) Revoke(code string) bool {
	p.mu.Lock()
	_, exists := p.codes[code]
	delete(p.codes, code)
	p.mu.Unlock()
	if exists {
		go p.save()
	}
	return exists
}

// ListValid 返回所有有效（未过期未耗尽）的验证码
func (p *Plugin) ListValid() []*CodeEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*CodeEntry, 0)
	for _, e := range p.codes {
		if e.IsValid() {
			clone := *e
			result = append(result, &clone)
		}
	}
	return result
}

// GC 清理过期验证码，返回清理数量
func (p *Plugin) GC() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for code, e := range p.codes {
		if !e.IsValid() {
			delete(p.codes, code)
			count++
		}
	}
	return count
}

// generateCode 生成随机验证码
func generateCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

func (p *Plugin) save() {
	if p.store == nil {
		return
	}
	p.mu.RLock()
	codes := make(map[string]*CodeEntry, len(p.codes))
	for k, v := range p.codes {
		clone := *v
		codes[k] = &clone
	}
	p.mu.RUnlock()
	if err := storage.Set(context.Background(), p.store, "codes", codes, 0); err != nil {
		logger.WithError(err).Warn("[VerifyCode] Failed to save codes")
	}
}

func (p *Plugin) load() {
	if p.store == nil {
		return
	}
	codes, err := storage.Get[map[string]*CodeEntry](context.Background(), p.store, "codes")
	if err != nil {
		return
	}
	p.mu.Lock()
	p.codes = codes
	p.mu.Unlock()
	logger.Infof("[VerifyCode] Loaded %d codes from storage", len(codes))
}
