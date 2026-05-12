package engine

// ChannelKey 是平台+会话的唯一标识，格式 "platform:chatID"。
type ChannelKey string

// MakeChannelKey 构造 ChannelKey。
func MakeChannelKey(platform, chatID string) ChannelKey {
	return ChannelKey(platform + ":" + chatID)
}
