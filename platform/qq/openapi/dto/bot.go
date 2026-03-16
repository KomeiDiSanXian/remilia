package dto

// BotInfo ..
//
// https://q.qq.com/qqbot/#/developer/developer-setting
type BotInfo struct {
	QQNum     uint64 // QQNum is the QQ number of the bot, used for identification
	AppID     uint64 // AppID is the application ID of the bot, used for authentication
	Token     string // Token is the token of the bot, used for authentication
	AppSecret string // AppSecret is the app secret of the bot, used for signing requests
	ServeAddr string // ServeAddr is the address of the webhook server, e.g. ":8080"
}

// NewBotInfo creates a new bot info
func NewBotInfo(qqNum, appID uint64, token, appSecret string) *BotInfo {
	b := &BotInfo{
		QQNum:     qqNum,
		AppID:     appID,
		Token:     token,
		AppSecret: appSecret,
	}
	if b.ServeAddr == "" {
		b.ServeAddr = ":9000" // Default address for the webhook server
	}
	return b
}
