package dto

// BotInfo ..
//
// https://q.qq.com/qqbot/#/developer/developer-setting
type BotInfo struct {
	QQNum     uint64 // QQ 号，用于标识机器人身份
	AppID     uint64 // 应用 ID，用于鉴权
	Token     string // Token，用于鉴权
	AppSecret string // App Secret，用于签名请求
	ServeAddr string // Webhook 服务监听地址，如 ":8080"
}

// NewBotInfo 创建新的 BotInfo
func NewBotInfo(qqNum, appID uint64, token, appSecret string) *BotInfo {
	b := &BotInfo{
		QQNum:     qqNum,
		AppID:     appID,
		Token:     token,
		AppSecret: appSecret,
	}
	if b.ServeAddr == "" {
		b.ServeAddr = ":9000" // Webhook 服务默认监听地址
	}
	return b
}
