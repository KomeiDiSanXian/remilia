package satori

// 对接实现核验说明（2026-08）
//
// 本客户端按 Satori 协议官方规范实现：https://satori.chat/zh-CN/protocol/
// 主要对接目标实现：
//
//   - LuckyLilliaBot（github.com/LLOneBot/LuckyLilliaBot，src/satori/）：
//     已实现 message.create/get/delete/list、channel.get/list/update/delete/mute、
//     user.channel.create、user.get、guild.get/list/approve、guild.member.*、
//     guild.role.list、guild.member.role.set、friend.list/approve/delete、
//     login.get、reaction.create/delete/list。参数用法与规范一致：
//     channel.mute 用 duration（毫秒）、reaction 用 emoji_id。
//   - Lagrange.Core 官方已移除 Satori 支持（master 现仅 Lagrange.Milky），
//     社区 Satori 对接主要走上述 LuckyLilliaBot 与 Chronocat。
//
// 已知差异（本客户端有、LuckyLilliaBot 未实现的 API，调用将返回
// "method not found" 404）：message.update、channel.create、
// guild.role.create/update/delete、guild.member.role.unset、interaction.respond
// （interaction.respond 本身在官方规范中也不存在，属实验性扩展）。
// 调用前可用 Login.Features（协议 /zh-CN/protocol/api.html#平台特性）探测。

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// 进阶 API 类型
// ─────────────────────────────────────────────────────────────────────────────

// UploadFile 描述一个待通过 upload.create API 上传的文件。
//
// 参见：https://satori.js.org/zh-CN/advanced/resource.html#api-upload-create
type UploadFile struct {
	// Name 是 multipart 字段标识符（Content-Disposition 中的 name，必需且不能重复）。
	// 返回字典的键与此字段一一对应。
	Name string

	// Filename 是文件名（Content-Disposition 中的 filename，可选）。
	Filename string

	// ContentType 是 MIME 类型（Content-Type，必需），如 "image/png"、"audio/ogg"。
	ContentType string

	// Data 是文件的二进制内容。
	Data io.Reader
}

// InternalCallRequest 是调用内部平台 API 的请求参数。
//
// 参见：https://satori.js.org/zh-CN/advanced/internal.html#api
type InternalCallRequest struct {
	// Method 是 HTTP 方法，如 "GET"、"POST"、"DELETE"。
	// 若为空，默认为 "POST"。
	Method string

	// Path 是 /{version}/internal/ 之后的路径，如 "channels/111222333"。
	// 开头的 "/" 会被自动去除。
	Path string

	// Body 是请求体（可选）。
	Body io.Reader

	// ContentType 是 Content-Type 请求头（可选）。
	// 若 Body 非 nil 且未指定，则默认为 "application/json"。
	ContentType string
}

// InternalCallResult 是内部 API 调用的响应结果。
type InternalCallResult struct {
	// StatusCode 是 HTTP 响应状态码。
	StatusCode int

	// Body 是响应体字节切片（原始平台 API 响应）。
	Body []byte

	// Header 是响应头（原始平台 API 响应头）。
	Header http.Header
}

// ─────────────────────────────────────────────────────────────────────────────
// SatoriClient
// ─────────────────────────────────────────────────────────────────────────────

// Client 是 Satori REST API 的轻量级 HTTP 客户端。
//
// 所有 API 方法均遵循 Satori HTTP RPC 规范：
//
//	POST /{version}/{resource}.{method}
//	Content-Type: application/json
//	Authorization: Bearer {token}     （如已配置）
//	Satori-Platform: {platform}
//	Satori-User-ID: {userID}
type Client struct {
	http     *http.Client
	baseURL  string // 例如 "http://localhost:5140"
	version  string // 例如 "v1"
	token    string
	platform string
	userID   string
}

// newClient 根据 Config 创建一个新的 Client。
func newClient(cfg Config) *Client {
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	version := cfg.Version
	if version == "" {
		version = "v1"
	}
	return &Client{
		http:     &http.Client{Timeout: timeout},
		baseURL:  httpBaseURL(cfg.ServerURL),
		version:  version,
		token:    cfg.Token,
		platform: cfg.Platform,
		userID:   cfg.UserID,
	}
}

// httpBaseURL 把 ServerURL 归一化为可用于 HTTP 调用的形式。
//
// Config.ServerURL 的字段文档明确写着支持 "ws://localhost:5140" 这种写法，
// wsConn.wsURL() 也确实兼容它。但 Client 此前直接把该字符串当作 REST 基地址，
// 而 http.Transport 只认 http/https，其余 scheme 一律
// "unsupported protocol scheme" 报错。
//
// 后果非常隐蔽：WebSocket 连得上、READY 正常、事件源源不断流入，适配器看起来
// 完全健康，但**每一条出站消息**都会失败。这里统一把 ws→http、wss→https。
func httpBaseURL(serverURL string) string {
	base := strings.TrimRight(serverURL, "/")
	switch {
	case strings.HasPrefix(base, "wss://"):
		return "https://" + strings.TrimPrefix(base, "wss://")
	case strings.HasPrefix(base, "ws://"):
		return "http://" + strings.TrimPrefix(base, "ws://")
	default:
		return base
	}
}

// call 执行一次 Satori HTTP RPC 调用，并将 JSON 响应解码到 result。
// 若 result 为 nil，则丢弃响应体。
func (c *Client) call(ctx stdctx.Context, resource, method string, params, result any) error {
	var body io.Reader
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("satori: 序列化请求参数: %w", err)
		}
		body = bytes.NewReader(data)
	} else {
		body = bytes.NewReader([]byte("{}"))
	}

	url := fmt.Sprintf("%s/%s/%s.%s", c.baseURL, c.version, resource, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("satori: 创建请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Satori-Platform", c.platform)
	req.Header.Set("Satori-User-ID", c.userID)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("satori: %s.%s: %w", resource, method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("satori: 读取响应体: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("satori: %s.%s: HTTP %d: %s", resource, method, resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("satori: %s.%s: 解码响应: %w", resource, method, err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 消息 API
// ─────────────────────────────────────────────────────────────────────────────

// MessageCreate 向频道发送消息。
// 返回创建的 Message 对象切片（单次发送因内容包含 <message> 元素可能产生多条消息）。
func (c *Client) MessageCreate(ctx stdctx.Context, channelID, content string) ([]*Message, error) {
	return c.MessageCreateWith(ctx, MessageCreateRequest{
		ChannelID: channelID,
		Content:   content,
	})
}

// MessageCreateRequest 是 message.create API 的完整参数结构。
//
// 与 MessageCreate 相比，MessageCreateWith 额外支持被动请求所需的 Referrer 字段。
//
// 参见：https://satori.chat/zh-CN/resources/message.html#api-message-create
//
//	https://satori.chat/zh-CN/advanced/passive.html
type MessageCreateRequest struct {
	// ChannelID 是目标频道 ID（必填）。
	ChannelID string `json:"channel_id"`
	// Content 是消息内容字符串（Satori XML 编码，必填）。
	Content string `json:"content"`
	// Referrer 是被动请求的来源信息（可选，实验性）。
	// 在需要被动回复的场景下，将来源事件的 event.referrer 原样传入此字段。
	// 参见：https://satori.chat/zh-CN/advanced/passive.html
	Referrer *json.RawMessage `json:"referrer,omitempty"`
}

// MessageCreateWith 使用完整参数向频道发送消息，支持被动请求 referrer。
//
// 适用于需要被动回复的场景（如 Lark 等对主动/被动操作加以区分的平台）：
//
//	msgs, err := client.MessageCreateWith(ctx, satori.MessageCreateRequest{
//	    ChannelID: "channelID",
//	    Content:   "<at id=\"123\"/>hello",
//	    Referrer:  event.Referrer(), // 来自收到的 satoriEvent
//	})
func (c *Client) MessageCreateWith(ctx stdctx.Context, req MessageCreateRequest) ([]*Message, error) {
	var result []*Message
	if err := c.call(ctx, "message", "create", req, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// MessageGet 根据 ID 获取指定消息。
func (c *Client) MessageGet(ctx stdctx.Context, channelID, messageID string) (*Message, error) {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
	}
	var result Message
	if err := c.call(ctx, "message", "get", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MessageDelete 撤回/删除指定消息。
func (c *Client) MessageDelete(ctx stdctx.Context, channelID, messageID string) error {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
	}
	return c.call(ctx, "message", "delete", params, nil)
}

// MessageUpdate 编辑已有消息。
func (c *Client) MessageUpdate(ctx stdctx.Context, channelID, messageID, content string) error {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"content":    content,
	}
	return c.call(ctx, "message", "update", params, nil)
}

// MessageListParams 包含 MessageList 的可选参数。
type MessageListParams struct {
	ChannelID string    `json:"channel_id"`
	Next      string    `json:"next,omitempty"`      // 分页令牌
	Direction Direction `json:"direction,omitempty"` // 查询方向
	Limit     int       `json:"limit,omitempty"`     // 消息数量限制
	Order     Order     `json:"order,omitempty"`     // 排序方式
}

// MessageList 返回频道中消息的双向分页列表。
func (c *Client) MessageList(ctx stdctx.Context, p MessageListParams) (*BidiList[*Message], error) {
	var result BidiList[*Message]
	if err := c.call(ctx, "message", "list", p, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 频道 API
// ─────────────────────────────────────────────────────────────────────────────

// ChannelGet 根据 ID 获取频道。
func (c *Client) ChannelGet(ctx stdctx.Context, channelID string) (*Channel, error) {
	params := map[string]any{"channel_id": channelID}
	var result Channel
	if err := c.call(ctx, "channel", "get", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ChannelList 列出群组中的所有频道。
func (c *Client) ChannelList(ctx stdctx.Context, guildID, next string) (*List[*Channel], error) {
	params := map[string]any{"guild_id": guildID}
	if next != "" {
		params["next"] = next
	}
	var result List[*Channel]
	if err := c.call(ctx, "channel", "list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ChannelCreate 在群组中创建新频道。
func (c *Client) ChannelCreate(ctx stdctx.Context, guildID string, data Channel) (*Channel, error) {
	params := map[string]any{
		"guild_id": guildID,
		"data":     data,
	}
	var result Channel
	if err := c.call(ctx, "channel", "create", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ChannelUpdate 修改频道信息。
func (c *Client) ChannelUpdate(ctx stdctx.Context, channelID string, data Channel) error {
	params := map[string]any{
		"channel_id": channelID,
		"data":       data,
	}
	return c.call(ctx, "channel", "update", params, nil)
}

// ChannelDelete 删除频道。
func (c *Client) ChannelDelete(ctx stdctx.Context, channelID string) error {
	params := map[string]any{"channel_id": channelID}
	return c.call(ctx, "channel", "delete", params, nil)
}

// ChannelMute 禁言/解除禁言频道，duration 为禁言时长（毫秒）。
// 传入 0 表示解除禁言。
func (c *Client) ChannelMute(ctx stdctx.Context, channelID string, duration time.Duration) error {
	params := map[string]any{
		"channel_id": channelID,
		"duration":   duration.Milliseconds(),
	}
	return c.call(ctx, "channel", "mute", params, nil)
}

// UserChannelCreate 与指定用户创建私聊频道。
func (c *Client) UserChannelCreate(ctx stdctx.Context, userID, guildID string) (*Channel, error) {
	params := map[string]any{"user_id": userID}
	if guildID != "" {
		params["guild_id"] = guildID
	}
	var result Channel
	if err := c.call(ctx, "user.channel", "create", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 用户 API
// ─────────────────────────────────────────────────────────────────────────────

// UserGet 获取用户信息。
func (c *Client) UserGet(ctx stdctx.Context, userID string) (*User, error) {
	params := map[string]any{"user_id": userID}
	var result User
	if err := c.call(ctx, "user", "get", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 群组 API
// ─────────────────────────────────────────────────────────────────────────────

// GuildGet 根据 ID 获取群组信息。
func (c *Client) GuildGet(ctx stdctx.Context, guildID string) (*Guild, error) {
	params := map[string]any{"guild_id": guildID}
	var result Guild
	if err := c.call(ctx, "guild", "get", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GuildList 返回机器人所属群组的分页列表。
func (c *Client) GuildList(ctx stdctx.Context, next string) (*List[*Guild], error) {
	params := map[string]any{}
	if next != "" {
		params["next"] = next
	}
	var result List[*Guild]
	if err := c.call(ctx, "guild", "list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GuildApprove 处理入群申请（approve=true 接受，false 拒绝）。
func (c *Client) GuildApprove(ctx stdctx.Context, messageID string, approve bool, comment string) error {
	params := map[string]any{
		"message_id": messageID,
		"approve":    approve,
		"comment":    comment,
	}
	return c.call(ctx, "guild", "approve", params, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// 群组成员 API
// ─────────────────────────────────────────────────────────────────────────────

// GuildMemberGet 获取指定群组成员信息。
func (c *Client) GuildMemberGet(ctx stdctx.Context, guildID, userID string) (*GuildMember, error) {
	params := map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
	}
	var result GuildMember
	if err := c.call(ctx, "guild.member", "get", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GuildMemberList 返回群组成员的分页列表。
func (c *Client) GuildMemberList(ctx stdctx.Context, guildID, next string) (*List[*GuildMember], error) {
	params := map[string]any{"guild_id": guildID}
	if next != "" {
		params["next"] = next
	}
	var result List[*GuildMember]
	if err := c.call(ctx, "guild.member", "list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GuildMemberKick 将成员移出群组。
// permanent=true 时同时禁止重新加入。
func (c *Client) GuildMemberKick(ctx stdctx.Context, guildID, userID string, permanent bool) error {
	params := map[string]any{
		"guild_id":  guildID,
		"user_id":   userID,
		"permanent": permanent,
	}
	return c.call(ctx, "guild.member", "kick", params, nil)
}

// GuildMemberMute 禁言/解除禁言群组成员。
// 传入 0 表示解除禁言。
func (c *Client) GuildMemberMute(ctx stdctx.Context, guildID, userID string, duration time.Duration) error {
	params := map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"duration": duration.Milliseconds(),
	}
	return c.call(ctx, "guild.member", "mute", params, nil)
}

// GuildMemberApprove 处理成员入群申请（approve=true 接受，false 拒绝）。
func (c *Client) GuildMemberApprove(ctx stdctx.Context, messageID string, approve bool, comment string) error {
	params := map[string]any{
		"message_id": messageID,
		"approve":    approve,
		"comment":    comment,
	}
	return c.call(ctx, "guild.member", "approve", params, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// 群组角色 API
// ─────────────────────────────────────────────────────────────────────────────

// GuildRoleList 返回群组角色的分页列表。
func (c *Client) GuildRoleList(ctx stdctx.Context, guildID, next string) (*List[*GuildRole], error) {
	params := map[string]any{"guild_id": guildID}
	if next != "" {
		params["next"] = next
	}
	var result List[*GuildRole]
	if err := c.call(ctx, "guild.role", "list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GuildRoleCreate 在群组中创建新角色。
func (c *Client) GuildRoleCreate(ctx stdctx.Context, guildID string, role GuildRole) (*GuildRole, error) {
	params := map[string]any{
		"guild_id": guildID,
		"role":     role,
	}
	var result GuildRole
	if err := c.call(ctx, "guild.role", "create", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GuildRoleUpdate 修改群组角色信息。
func (c *Client) GuildRoleUpdate(ctx stdctx.Context, guildID, roleID string, role GuildRole) error {
	params := map[string]any{
		"guild_id": guildID,
		"role_id":  roleID,
		"role":     role,
	}
	return c.call(ctx, "guild.role", "update", params, nil)
}

// GuildRoleDelete 删除群组角色。
func (c *Client) GuildRoleDelete(ctx stdctx.Context, guildID, roleID string) error {
	params := map[string]any{
		"guild_id": guildID,
		"role_id":  roleID,
	}
	return c.call(ctx, "guild.role", "delete", params, nil)
}

// GuildMemberRoleSet 为群组成员赋予角色。
func (c *Client) GuildMemberRoleSet(ctx stdctx.Context, guildID, userID, roleID string) error {
	params := map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"role_id":  roleID,
	}
	return c.call(ctx, "guild.member.role", "set", params, nil)
}

// GuildMemberRoleUnset 移除群组成员的角色。
func (c *Client) GuildMemberRoleUnset(ctx stdctx.Context, guildID, userID, roleID string) error {
	params := map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"role_id":  roleID,
	}
	return c.call(ctx, "guild.member.role", "unset", params, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// 好友 API
// ─────────────────────────────────────────────────────────────────────────────

// FriendList 返回好友的分页列表。
func (c *Client) FriendList(ctx stdctx.Context, next string) (*List[*User], error) {
	params := map[string]any{}
	if next != "" {
		params["next"] = next
	}
	var result List[*User]
	if err := c.call(ctx, "friend", "list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// FriendApprove 接受或拒绝好友申请。
func (c *Client) FriendApprove(ctx stdctx.Context, messageID string, approve bool, comment string) error {
	params := map[string]any{
		"message_id": messageID,
		"approve":    approve,
		"comment":    comment,
	}
	return c.call(ctx, "friend", "approve", params, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// 登录 API
// ─────────────────────────────────────────────────────────────────────────────

// LoginGet 获取当前登录信息。
func (c *Client) LoginGet(ctx stdctx.Context) (*Login, error) {
	var result Login
	if err := c.call(ctx, "login", "get", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 表态 API
// ─────────────────────────────────────────────────────────────────────────────

// ReactionCreate 为消息添加表态。
func (c *Client) ReactionCreate(ctx stdctx.Context, channelID, messageID, emojiID string) error {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji_id":   emojiID,
	}
	return c.call(ctx, "reaction", "create", params, nil)
}

// ReactionDelete 删除消息上的表态。
// userID 为可选参数；若为空字符串则删除机器人自身的表态。
func (c *Client) ReactionDelete(ctx stdctx.Context, channelID, messageID, emojiID, userID string) error {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji_id":   emojiID,
	}
	if userID != "" {
		params["user_id"] = userID
	}
	return c.call(ctx, "reaction", "delete", params, nil)
}

// ReactionList 返回消息表态的分页用户列表。
func (c *Client) ReactionList(ctx stdctx.Context, channelID, messageID, emojiID, next string) (*List[*User], error) {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji_id":   emojiID,
	}
	if next != "" {
		params["next"] = next
	}
	var result List[*User]
	if err := c.call(ctx, "reaction", "list", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 交互 API
// ─────────────────────────────────────────────────────────────────────────────

// InteractionRespond 响应交互事件（实验性）。
func (c *Client) InteractionRespond(ctx stdctx.Context, interactionID, content string) error {
	params := map[string]any{
		"id":      interactionID,
		"content": content,
	}
	return c.call(ctx, "interaction", "respond", params, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// 资源上传 API（实验性）
// ─────────────────────────────────────────────────────────────────────────────

// UploadCreate 将一组文件以 multipart/form-data 格式上传到 Satori SDK，
// 并返回一个以文件标识符为键、以可用 URL 为值的字典。
//
// 路由：POST /{version}/upload.create
//
// 若平台支持文件上传，SDK 返回平台原生 URL；
// 否则返回以 "internal:" 开头的内部链接（有效期约 5 分钟）。
//
// 参见：https://satori.js.org/zh-CN/advanced/resource.html#api-upload-create
func (c *Client) UploadCreate(ctx stdctx.Context, files ...UploadFile) (map[string]string, error) {
	if len(files) == 0 {
		return map[string]string{}, nil
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range files {
		h := make(textproto.MIMEHeader)
		cd := fmt.Sprintf(`form-data; name=%q`, f.Name)
		if f.Filename != "" {
			cd = fmt.Sprintf(`form-data; name=%q; filename=%q`, f.Name, f.Filename)
		}
		h.Set("Content-Disposition", cd)
		if f.ContentType != "" {
			h.Set("Content-Type", f.ContentType)
		} else {
			h.Set("Content-Type", "application/octet-stream")
		}
		part, err := mw.CreatePart(h)
		if err != nil {
			return nil, fmt.Errorf("satori: upload.create: 创建 multipart 部分 %q: %w", f.Name, err)
		}
		if f.Data != nil {
			if _, err := io.Copy(part, f.Data); err != nil {
				return nil, fmt.Errorf("satori: upload.create: 写入文件 %q: %w", f.Name, err)
			}
		}
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("satori: upload.create: 关闭 multipart writer: %w", err)
	}

	u := fmt.Sprintf("%s/%s/upload.create", c.baseURL, c.version)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return nil, fmt.Errorf("satori: upload.create: 创建请求: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Satori-Platform", c.platform)
	req.Header.Set("Satori-User-ID", c.userID)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("satori: upload.create: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("satori: upload.create: 读取响应体: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("satori: upload.create: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]string
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("satori: upload.create: 解码响应: %w", err)
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 代理路由（实验性）
// ─────────────────────────────────────────────────────────────────────────────

// ProxyFetch 通过 SDK 代理路由下载指定 URL 的资源，
// 支持内部链接（internal:...）和含防盗链机制的外部链接。
//
// 路由：GET /{version}/proxy/{resourceURL}
//
// 该接口不需要 Satori-Platform 和 Satori-User-ID 请求头。
// 返回资源的原始字节、响应头及错误。
//
// 参见：https://satori.js.org/zh-CN/advanced/resource.html#proxy-route
func (c *Client) ProxyFetch(ctx stdctx.Context, resourceURL string) ([]byte, http.Header, error) {
	u := fmt.Sprintf("%s/%s/proxy/%s", c.baseURL, c.version, resourceURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("satori: proxy: 创建请求: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	// 代理路由不需要 Satori-Platform 和 Satori-User-ID

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("satori: proxy: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("satori: proxy: 读取响应体: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("satori: proxy: HTTP %d", resp.StatusCode)
	}
	return body, resp.Header, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 元信息 API（实验性）
// ─────────────────────────────────────────────────────────────────────────────

// callMeta 执行一次元信息 API 调用（不携带 Satori-Platform 和 Satori-User-ID 请求头）。
// path 为 /{version}/ 后的路径，如 "meta" 或 "meta/webhook.create"。
func (c *Client) callMeta(ctx stdctx.Context, path string, params, result any) error {
	var body io.Reader
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("satori: 序列化请求参数: %w", err)
		}
		body = bytes.NewReader(data)
	} else {
		body = bytes.NewReader([]byte("{}"))
	}

	u := fmt.Sprintf("%s/%s/%s", c.baseURL, c.version, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return fmt.Errorf("satori: 创建请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	// 元信息 API 不需要 Satori-Platform 和 Satori-User-ID

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("satori: %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("satori: 读取响应体: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("satori: %s: HTTP %d: %s", path, resp.StatusCode, string(respBody))
	}
	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("satori: %s: 解码响应: %w", path, err)
		}
	}
	return nil
}

// MetaGet 获取 SDK 元信息，包含所有登录信息和代理路由列表。
//
// 路由：POST /{version}/meta
//
// 在 WebHook 推送模式下，应用启动时应调用此方法获取初始元信息。
//
// 参见：https://satori.js.org/zh-CN/advanced/meta.html
func (c *Client) MetaGet(ctx stdctx.Context) (*Meta, error) {
	var result Meta
	if err := c.callMeta(ctx, "meta", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MetaWebhookCreate 向 SDK 注册一个 WebHook 推送地址（可选功能）。
//
// 路由：POST /{version}/meta/webhook.create
//
// webhookURL 是接收事件推送的 HTTP 地址；
// token 是可选的鉴权令牌，SDK 会在推送请求的 Authorization 头中携带。
//
// 参见：https://satori.js.org/zh-CN/advanced/meta.html#创建-webhook
func (c *Client) MetaWebhookCreate(ctx stdctx.Context, webhookURL, token string) error {
	params := map[string]any{"url": webhookURL}
	if token != "" {
		params["token"] = token
	}
	return c.callMeta(ctx, "meta/webhook.create", params, nil)
}

// MetaWebhookDelete 从 SDK 移除指定的 WebHook 推送地址（可选功能）。
//
// 路由：POST /{version}/meta/webhook.delete
//
// 参见：https://satori.js.org/zh-CN/advanced/meta.html#移除-webhook
func (c *Client) MetaWebhookDelete(ctx stdctx.Context, webhookURL string) error {
	params := map[string]any{"url": webhookURL}
	return c.callMeta(ctx, "meta/webhook.delete", params, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// 内部 API（实验性，跨平台原生接口）
// ─────────────────────────────────────────────────────────────────────────────

// InternalCall 代理一次对平台原生 API 的调用。
//
// 路由：{Method} /{version}/internal/{Path}
//
// 整个请求和响应格式与平台原生 API 保持一致；
// 额外需要 Satori-Platform 和 Satori-User-ID 请求头（由客户端自动附加）。
//
// 示例（代理 Discord 删除频道请求）：
//
//	result, err := client.InternalCall(ctx, satori.InternalCallRequest{
//	    Method: "DELETE",
//	    Path:   "channels/111222333",
//	})
//
// 参见：https://satori.js.org/zh-CN/advanced/internal.html#api
func (c *Client) InternalCall(ctx stdctx.Context, req InternalCallRequest) (*InternalCallResult, error) {
	method := req.Method
	if method == "" {
		method = http.MethodPost
	}
	path := strings.TrimPrefix(req.Path, "/")
	u := fmt.Sprintf("%s/%s/internal/%s", c.baseURL, c.version, path)

	httpReq, err := http.NewRequestWithContext(ctx, method, u, req.Body)
	if err != nil {
		return nil, fmt.Errorf("satori: internal: 创建请求: %w", err)
	}
	if req.ContentType != "" {
		httpReq.Header.Set("Content-Type", req.ContentType)
	} else if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.token)
	}
	httpReq.Header.Set("Satori-Platform", c.platform)
	httpReq.Header.Set("Satori-User-ID", c.userID)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("satori: internal %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("satori: internal: 读取响应体: %w", err)
	}
	return &InternalCallResult{
		StatusCode: resp.StatusCode,
		Body:       body,
		Header:     resp.Header,
	}, nil
}
