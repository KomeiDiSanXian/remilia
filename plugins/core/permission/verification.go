package permission

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// VerificationCode 验证码信息
type VerificationCode struct {
	Code      string    // 验证码
	Role      string    // 授予的角色
	ExpiresAt time.Time // 过期时间
	UsedBy    string    // 使用者ID（空表示未使用）
	CreatedAt time.Time // 创建时间
	MaxUses   int       // 最大使用次数（0表示一次性，-1表示无限次）
	UseCount  int       // 已使用次数
}

// VerificationManager 验证码管理器
type VerificationManager struct {
	mu    sync.RWMutex
	codes map[string]*VerificationCode // code -> VerificationCode
}

// NewVerificationManager 创建验证码管理器
func NewVerificationManager() *VerificationManager {
	return &VerificationManager{
		codes: make(map[string]*VerificationCode),
	}
}

// GenerateCode 生成验证码
// role: 验证码授予的角色
// expiry: 过期时间（从现在开始的持续时间）
// maxUses: 最大使用次数（0=一次性，-1=无限次）
func (vm *VerificationManager) GenerateCode(role string, expiry time.Duration, maxUses int) (string, error) {
	// 生成随机验证码（6位字符）
	code, err := generateRandomCode(6)
	if err != nil {
		return "", fmt.Errorf("failed to generate code: %w", err)
	}

	vm.mu.Lock()
	defer vm.mu.Unlock()

	// 确保验证码唯一性
	for vm.codes[code] != nil {
		code, err = generateRandomCode(6)
		if err != nil {
			return "", fmt.Errorf("failed to generate unique code: %w", err)
		}
	}

	// 创建验证码信息
	vm.codes[code] = &VerificationCode{
		Code:      code,
		Role:      role,
		ExpiresAt: time.Now().Add(expiry),
		CreatedAt: time.Now(),
		MaxUses:   maxUses,
		UseCount:  0,
	}

	return code, nil
}

// VerifyCode 验证并使用验证码
// 返回: role, success, error
func (vm *VerificationManager) VerifyCode(code, userID string) (string, bool, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	vc, exists := vm.codes[code]
	if !exists {
		return "", false, fmt.Errorf("invalid verification code")
	}

	// 检查是否过期
	if time.Now().After(vc.ExpiresAt) {
		delete(vm.codes, code)
		return "", false, fmt.Errorf("verification code expired")
	}

	// 检查使用次数
	if vc.MaxUses == 0 {
		// 一次性验证码
		if vc.UseCount > 0 {
			return "", false, fmt.Errorf("verification code already used")
		}
	} else if vc.MaxUses > 0 {
		// 有限次数
		if vc.UseCount >= vc.MaxUses {
			return "", false, fmt.Errorf("verification code usage limit exceeded")
		}
	}
	// MaxUses == -1 表示无限次使用

	// 记录使用
	vc.UseCount++
	vc.UsedBy = userID

	// 如果是一次性验证码，使用后删除
	if vc.MaxUses == 0 {
		role := vc.Role
		delete(vm.codes, code)
		return role, true, nil
	}

	return vc.Role, true, nil
}

// RevokeCode 撤销验证码
func (vm *VerificationManager) RevokeCode(code string) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if _, exists := vm.codes[code]; !exists {
		return fmt.Errorf("verification code not found")
	}

	delete(vm.codes, code)
	return nil
}

// ListCodes 列出所有有效的验证码
func (vm *VerificationManager) ListCodes() []*VerificationCode {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	result := make([]*VerificationCode, 0, len(vm.codes))
	now := time.Now()

	for _, vc := range vm.codes {
		// 只返回未过期的验证码
		if now.Before(vc.ExpiresAt) {
			// 复制一份避免外部修改
			result = append(result, &VerificationCode{
				Code:      vc.Code,
				Role:      vc.Role,
				ExpiresAt: vc.ExpiresAt,
				UsedBy:    vc.UsedBy,
				CreatedAt: vc.CreatedAt,
				MaxUses:   vc.MaxUses,
				UseCount:  vc.UseCount,
			})
		}
	}

	return result
}

// CleanupExpired 清理过期的验证码
func (vm *VerificationManager) CleanupExpired() int {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	now := time.Now()
	count := 0

	for code, vc := range vm.codes {
		if now.After(vc.ExpiresAt) {
			delete(vm.codes, code)
			count++
		}
	}

	return count
}

// GetCodeInfo 获取验证码信息
func (vm *VerificationManager) GetCodeInfo(code string) (*VerificationCode, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	vc, exists := vm.codes[code]
	if !exists {
		return nil, fmt.Errorf("verification code not found")
	}

	// 返回副本
	return &VerificationCode{
		Code:      vc.Code,
		Role:      vc.Role,
		ExpiresAt: vc.ExpiresAt,
		UsedBy:    vc.UsedBy,
		CreatedAt: vc.CreatedAt,
		MaxUses:   vc.MaxUses,
		UseCount:  vc.UseCount,
	}, nil
}

// generateRandomCode 生成随机验证码
// length: 验证码长度
func generateRandomCode(length int) (string, error) {
	// 使用更友好的字符集（避免易混淆字符：0 O I l 1）
	const charset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	for i := range bytes {
		bytes[i] = charset[int(bytes[i])%len(charset)]
	}

	return string(bytes), nil
}

// GenerateSecureToken 生成安全令牌（用于高安全场景）
func GenerateSecureToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}
