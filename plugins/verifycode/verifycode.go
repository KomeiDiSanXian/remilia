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
//	pm.RegisterV2(verifycode.New())
//	vc := ctx.MustGet("verifycode").(*verifycode.Plugin)
//	code, _ := vc.Generate(verifycode.Config{Role: "vip", TTL: 24*time.Hour, MaxUses: 1})
//	role, err := vc.Verify(userID, codeStr)
package verifycode

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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

// storageBackend 避免直接依赖 storage 包
type storageBackend interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, ttl time.Duration) error
}

// Plugin 验证码插件
type Plugin struct {
	mu       sync.RWMutex
	codes    map[string]*CodeEntry // code -> entry
	onVerify OnVerifyHook
	storage  storageBackend
}

// NewPlugin 创建 Plugin 实例
func NewPlugin(onVerify OnVerifyHook) *Plugin {
	return &Plugin{
		codes:    make(map[string]*CodeEntry),
		onVerify: onVerify,
	}
}

// New 创建验证码插件描述符
func New(onVerify OnVerifyHook) *plugin.PluginDescriptor {
	p := NewPlugin(onVerify)
	return Descriptor(p)
}

// Descriptor 从已有 Plugin 创建描述符
func Descriptor(p *Plugin) *plugin.PluginDescriptor {
	return &plugin.PluginDescriptor{
		Name:        "verifycode",
		Version:     "1.0.0",
		Author:      "Remilia Team",
		Description: "验证码插件，支持角色绑定、有效期和使用次数控制",
		Category:    "安全",
		Tags:        []string{"安全", "验证码", "授权"},
		Deps:        []string{},
		HelpText: `验证码插件使用说明：
  vc := verifycode.NewPlugin(func(userID, role string) error {
      // 授予角色
      return permPlugin.SetRole(userID, role)
  })
  pm.RegisterV2(verifycode.Descriptor(vc))

  // 生成验证码
  code, _ := vc.Generate(verifycode.CodeConfig{
      Role:    "vip",
      TTL:     24 * time.Hour,
      MaxUses: 1,  // 一次性
  })

  // 验证验证码
  role, err := vc.Verify(userID, code)`,
		Setup: func(ctx *plugin.SetupContext) error {
			logger.Info("[VerifyCode] Plugin loaded")
			ctx.Manager.GetContainer().Register("verifycode", p)
			// 可选持久化
			if storageRaw, ok := ctx.Manager.GetContainer().Get("storage"); ok {
				if sb, ok := storageRaw.(storageBackend); ok {
					p.storage = sb
					p.load()
				}
			}
			return nil
		},
		Teardown: func() error {
			p.save()
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
	for _, uid := range entry.UsedBy {
		if uid == userID {
			p.mu.Unlock()
			return "", fmt.Errorf("code already used by this user")
		}
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

// ----- 持久化 -----

func (p *Plugin) save() {
	if p.storage == nil {
		return
	}
	p.mu.RLock()
	data, err := json.Marshal(p.codes)
	p.mu.RUnlock()
	if err != nil {
		return
	}
	_ = p.storage.Set("verifycode:codes", data, 0)
}

func (p *Plugin) load() {
	if p.storage == nil {
		return
	}
	data, err := p.storage.Get("verifycode:codes")
	if err != nil {
		return
	}
	var codes map[string]*CodeEntry
	if err := json.Unmarshal(data, &codes); err != nil {
		return
	}
	p.mu.Lock()
	p.codes = codes
	p.mu.Unlock()
	logger.Infof("[VerifyCode] Loaded %d codes from storage", len(codes))
}
