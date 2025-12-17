package global

import (
	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

var (
	// Info Bot 信息，需要先调用 InitFromConfig 初始化
	Info *dto.BotInfo
)

// InitFromConfig 从配置初始化 Bot 信息
func InitFromConfig(cfg *config.Config) {
	Info = dto.NewBotInfo(
		cfg.Bot.AppID,
		cfg.Bot.BotID,
		cfg.Bot.Token,
		cfg.Bot.Secret,
	)
}

// MustInitFromConfig 从配置初始化，失败则 panic
func MustInitFromConfig(cfg *config.Config) {
	if cfg == nil {
		panic("config is nil")
	}
	InitFromConfig(cfg)
	if Info == nil {
		panic("failed to initialize bot info")
	}
}
