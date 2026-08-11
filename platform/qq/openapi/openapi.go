// Package openapi 提供 QQ 机器人 OpenAPI 客户端。
package openapi

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"

	"github.com/KomeiDiSanXian/remilia/infra/httpclient"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/constant"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/tidwall/gjson"
)

// Client 对 OpenAPI 的实现
type Client struct {
	tm *token.Manager
}

// New 创建 OpenAPI 服务
func New(manager *token.Manager) *Client {
	return &Client{tm: manager}
}

// authHeader 返回当前 access token 鉴权头值。
func (api *Client) authHeader() string {
	return fmt.Sprintf("QQBot %s", api.tm.GetToken())
}

// Post 发送一个 post 请求到 url，自动添加 Authorization 头，ctx 用于传播超时/取消。
func (api *Client) Post(ctx context.Context, url string, data any) (gjson.Result, error) {
	// 必须传播错误并监听调用方 ctx。
	//
	// 此前这里直接丢弃返回值：token 未就绪时 authHeader() 会拼出
	// "QQBot "（空 token）照发不误，QQ 一律回 401，调用方拿到的是一个
	// 看不出所以然的平台错误；同时固定 30 秒的等待完全无视调用方自己的
	// deadline。
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	result, err := httpclient.Post(url).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader()).
		SetJSON(data).
		DoJSON()
	if err != nil {
		logger.WithError(err).WithField("url", url).Error("[OpenAPI] Post failed")
		return gjson.Result{}, err
	}
	return result, nil
}

// Put 发送一个 put 请求到 url，自动添加 Authorization 头。
// 若 data 为 nil（含类型化的 nil 指针），则发送无请求体的 PUT 请求
// （适用于如表情表态等不需要请求体的接口）。
func (api *Client) Put(ctx context.Context, url string, data any) (gjson.Result, error) {
	// 必须传播错误并监听调用方 ctx。
	//
	// 此前这里直接丢弃返回值：token 未就绪时 authHeader() 会拼出
	// "QQBot "（空 token）照发不误，QQ 一律回 401，调用方拿到的是一个
	// 看不出所以然的平台错误；同时固定 30 秒的等待完全无视调用方自己的
	// deadline。
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	req := httpclient.Put(url).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader())
	if !isNilPayload(data) {
		req = req.SetJSON(data)
	}
	result, err := req.DoJSON()
	if err != nil {
		logger.WithError(err).WithField("url", url).Error("[OpenAPI] Put failed")
		return gjson.Result{}, err
	}
	return result, nil
}

// isNilPayload 判断 data 是否为 nil，包括"类型化的 nil"（如
// (*dto.AddMemberRoleRequest)(nil) 装入 any 后 data != nil 但实际无值）。
// 此前 AddGuildMemberRole 在 channelID 为空时会把这种类型化 nil 传给 Put，
// 导致请求体被序列化成字面量 "null"，与 PinMessage/AddReaction 等
// 无请求体 PUT 的行为不一致。
func isNilPayload(data any) bool {
	if data == nil {
		return true
	}
	v := reflect.ValueOf(data)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// Patch 发送一个 patch 请求到 url，自动添加 Authorization 头。
func (api *Client) Patch(ctx context.Context, url string, data any) (gjson.Result, error) {
	// 必须传播错误并监听调用方 ctx。
	//
	// 此前这里直接丢弃返回值：token 未就绪时 authHeader() 会拼出
	// "QQBot "（空 token）照发不误，QQ 一律回 401，调用方拿到的是一个
	// 看不出所以然的平台错误；同时固定 30 秒的等待完全无视调用方自己的
	// deadline。
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	result, err := httpclient.Patch(url).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader()).
		SetJSON(data).
		DoJSON()
	if err != nil {
		logger.WithError(err).WithField("url", url).Error("[OpenAPI] Patch failed")
		return gjson.Result{}, err
	}
	return result, nil
}

// Get 发送一个 get 请求到 url，自动添加 Authorization 头。
func (api *Client) Get(ctx context.Context, url string) (gjson.Result, error) {
	// 必须传播错误并监听调用方 ctx。
	//
	// 此前这里直接丢弃返回值：token 未就绪时 authHeader() 会拼出
	// "QQBot "（空 token）照发不误，QQ 一律回 401，调用方拿到的是一个
	// 看不出所以然的平台错误；同时固定 30 秒的等待完全无视调用方自己的
	// deadline。
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	result, err := httpclient.Get(url).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader()).
		DoJSON()
	if err != nil {
		logger.WithError(err).WithField("url", url).Error("[OpenAPI] Get failed")
		return gjson.Result{}, err
	}
	return result, nil
}

// Delete 发送一个 delete 请求到 url，自动添加 Authorization 头，ctx 用于传播超时/取消。
func (api *Client) Delete(ctx context.Context, url string) (gjson.Result, error) {
	// 必须传播错误并监听调用方 ctx。
	//
	// 此前这里直接丢弃返回值：token 未就绪时 authHeader() 会拼出
	// "QQBot "（空 token）照发不误，QQ 一律回 401，调用方拿到的是一个
	// 看不出所以然的平台错误；同时固定 30 秒的等待完全无视调用方自己的
	// deadline。
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	resp, err := httpclient.Delete(url).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader()).
		SetHeader("Content-Type", "application/json").
		Do()
	if err != nil {
		logger.WithError(err).WithField("url", url).Error("[OpenAPI] Delete failed")
		return gjson.Result{}, err
	}
	defer resp.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		logger.WithField("status", resp.Status).WithField("url", url).Error("[OpenAPI] Delete failed")
		return gjson.Result{}, fmt.Errorf("status code %d: %s", resp.StatusCode, resp.Status)
	}
	if resp.StatusCode == http.StatusNoContent {
		return gjson.Result{}, nil
	}
	return resp.JSON()
}

// ── 消息发送 ────────────────────────────────────────────────────────────────

// SingleChat 向指定用户发送单聊（C2C）消息。
//
// openid 为目标用户的 OpenID。
// 支持文本、Markdown、富媒体、ARK 模板、键盘按钮等消息类型。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_users_user_openid_messages.post.html
func (api *Client) SingleChat(ctx context.Context, openid string, msg *dto.Message) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.SingleChatURL, openid), msg)
}

// GroupChat 向指定群发送消息。
//
// groupOpenid 为群 OpenID。
// 支持文本、Markdown、卡片、富媒体、键盘按钮等消息类型。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_messages.post.html
func (api *Client) GroupChat(ctx context.Context, groupOpenid string, msg *dto.Message) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupChatURL, groupOpenid), msg)
}

// ChannelChat 向文字子频道发送消息。
func (api *Client) ChannelChat(ctx context.Context, channelID string, msg *dto.GuildMessage) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.ChannelChatURL, channelID), msg)
}

// DMChat 向频道私信（DM）会话发送消息。
// guildID 为通过 CreateDirectMessageSession 创建会话后返回的私信 guild_id。
func (api *Client) DMChat(ctx context.Context, guildID string, msg *dto.GuildMessage) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.DMChatURL, guildID), msg)
}

// SingleRichMedia 上传单聊富媒体文件（图片、视频、语音、文件）。
//
// 上传后返回 file_info，用于发消息接口（msg_type=7）携带 media.file_info 发送。
// 支持 URL 上传和 base64 数据上传。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_users_user_openid_files.post.html
func (api *Client) SingleRichMedia(ctx context.Context, openid string, media *dto.Media) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.SingleRichMediaURL, openid), media)
}

// GroupRichMedia 上传群聊富媒体文件。
//
// 与 SingleRichMedia 用法一致，但上传的文件只能在群聊场景使用。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_files.post.html
func (api *Client) GroupRichMedia(ctx context.Context, groupOpenid string, media *dto.Media) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupRichMediaURL, groupOpenid), media)
}

// SingleStreamChat 流式分批发送单聊消息。
//
// 每个分片使用相同 stream_msg_id，index 从 0 递增。
// 适合 AI 回复等需要逐段展示内容的场景。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_users_user_openid_stream_messages.post.html
func (api *Client) SingleStreamChat(ctx context.Context, openid string, msg *dto.StreamMessage) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.StreamSingleChatURL, openid), msg)
}

// ── 富媒体分片上传 ───────────────────────────────────────────────────────────

// UserUploadPrepare 单聊富媒体预上传（分片上传第一步）。
//
// 传入文件信息和校验值，返回 upload_id、block_size 和分片预签名 URL。
//
// https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/rich-media.html#分片上传（推荐）
func (api *Client) UserUploadPrepare(ctx context.Context, openid string, req *dto.UploadPrepareRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.UserUploadPrepareURL, openid), req)
}

// GroupUploadPrepare 群聊富媒体预上传（分片上传第一步）。
func (api *Client) GroupUploadPrepare(ctx context.Context, groupID string, req *dto.UploadPrepareRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupUploadPrepareURL, groupID), req)
}

// UserUploadPartFinish 单聊分片上传完成确认（分片上传第三步）。
//
// 每个分片 PUT 到预签名 URL 后调用此接口通知服务端该分片已完成。
func (api *Client) UserUploadPartFinish(ctx context.Context, openid string, req *dto.UploadPartFinishRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.UserUploadPartFinishURL, openid), req)
}

// GroupUploadPartFinish 群聊分片上传完成确认（分片上传第三步）。
func (api *Client) GroupUploadPartFinish(ctx context.Context, groupID string, req *dto.UploadPartFinishRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupUploadPartFinishURL, groupID), req)
}

// ── 消息撤回 ────────────────────────────────────────────────────────────────

// SingleReset 撤回单聊消息。
//
// 只能撤回机器人自己发送且不超过 2 分钟的消息。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_users_user_openid_messages_message_id.delete.html
func (api *Client) SingleReset(ctx context.Context, openid, messageID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.SingleResetURL, openid, messageID))
}

// GroupReset 撤回群聊消息。
//
// 只能撤回机器人自己发送且不超过 2 分钟的消息。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_messages_message_id.delete.html
func (api *Client) GroupReset(ctx context.Context, groupOpenid, messageID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.GroupResetURL, groupOpenid, messageID))
}

// ChannelReset 撤回文字子频道消息（仅私域机器人可用）。
// hidetip=true 时隐藏客户端侧的撤回灰条提示；false（默认）时显示。
func (api *Client) ChannelReset(ctx context.Context, channelID, messageID string, hidetip bool) (gjson.Result, error) {
	url := fmt.Sprintf(constant.ChannelResetURL, channelID, messageID)
	return api.Delete(ctx, fmt.Sprintf("%s?hidetip=%t", url, hidetip))
}

// DMReset 撤回频道私信消息（仅私域机器人可用，只能撤回机器人自己发送的私信）。
// hidetip=true 时隐藏客户端侧的撤回灰条提示；false（默认）时显示。
func (api *Client) DMReset(ctx context.Context, guildID, messageID string, hidetip bool) (gjson.Result, error) {
	url := fmt.Sprintf(constant.DMResetURL, guildID, messageID)
	return api.Delete(ctx, fmt.Sprintf("%s?hidetip=%t", url, hidetip))
}

// ── 互动事件 ────────────────────────────────────────────────────────────────

// RespondInteraction 回应 INTERACTION_CREATE 事件（PUT /interactions/{interaction_id}）。
//
// 参考官方 botgo SDK（openapi/v1/interaction.go）：必须携带 X-Callback-AppID
// 请求头标识机器人身份，否则互动事件不会被正确关联，用户侧会显示
// "请求第三方失败"。
//
// 返回错误的情形：
//   - HTTP 状态码非 2xx（如 401 鉴权失败、404 接口不存在）；
//   - 响应体携带非零 err_code（QQ 部分错误响应 HTTP 状态码也是 200，
//     必须显式检查 err_code，否则调用方会误以为回应成功——
//     互动事件未回应时用户侧会显示"请求第三方失败"）。
func (api *Client) RespondInteraction(ctx context.Context, interactionID string, code int) (gjson.Result, error) {
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	url := fmt.Sprintf(constant.InteractionURL, interactionID)
	resp, err := httpclient.Put(url).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader()).
		SetHeader("X-Callback-AppID", strconv.FormatUint(api.tm.GetAppID(), 10)).
		SetJSON(&dto.InteractionResponse{Code: code}).
		Do()
	if err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: RespondInteraction %s: %w", url, err)
	}
	defer resp.Close()

	result, err := resp.JSON()
	if err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: RespondInteraction %s: parse response: %w", url, err)
	}
	if !resp.IsSuccess() {
		return result, fmt.Errorf("qq openapi: RespondInteraction %s: HTTP %d: %s", url, resp.StatusCode, result.Get("message").String())
	}
	if ec := result.Get("err_code").Int(); ec != 0 {
		return result, fmt.Errorf("qq openapi: RespondInteraction %s: err_code=%d message=%s", url, ec, result.Get("message").String())
	}
	return result, nil
}

// ── 表情表态（仅频道）────────────────────────────────────────────────────────

// AddReaction 对频道消息发表表情表态（PUT /channels/{channel_id}/messages/{message_id}/reactions/{type}/{id}）。
func (api *Client) AddReaction(ctx context.Context, channelID, messageID string, emojiType int, emojiID string) (gjson.Result, error) {
	return api.Put(ctx, fmt.Sprintf(constant.ChannelMessageReactionURL, channelID, messageID, emojiType, emojiID), nil)
}

// DeleteReaction 删除机器人的表情表态（DELETE /channels/{channel_id}/messages/{message_id}/reactions/{type}/{id}）。
func (api *Client) DeleteReaction(ctx context.Context, channelID, messageID string, emojiType int, emojiID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.ChannelMessageReactionURL, channelID, messageID, emojiType, emojiID))
}

// GetReactionUsers 获取消息表情表态的用户列表（GET /channels/{channel_id}/messages/{message_id}/reactions/{type}/{id}）。
// cookie 为分页游标（首次请求传空字符串），limit 为每页数量（默认 20，最大 50）。
func (api *Client) GetReactionUsers(ctx context.Context, channelID, messageID string, emojiType int, emojiID, cookie string, limit int) (gjson.Result, error) {
	url := fmt.Sprintf(constant.ChannelMessageReactionURL, channelID, messageID, emojiType, emojiID)
	url += fmt.Sprintf("?cookie=%s&limit=%d", cookie, limit)
	return api.Get(ctx, url)
}

// ── 频道管理 ─────────────────────────────────────────────────────────────────

// GetMe 获取当前用户（机器人）详情。
func (api *Client) GetMe(ctx context.Context) (gjson.Result, error) {
	return api.Get(ctx, constant.UsersMeURL)
}

// GetMyGuilds 获取机器人已加入的频道列表（分页）。
// before/after 为游标（guild_id），均为空时从头拉取；limit=0 时使用平台默认值（100）。
func (api *Client) GetMyGuilds(ctx context.Context, before, after string, limit int) (gjson.Result, error) {
	url := constant.UsersMeGuildsURL
	sep := "?"
	if before != "" {
		url += sep + "before=" + before
		sep = "&"
	}
	if after != "" {
		url += sep + "after=" + after
		sep = "&"
	}
	if limit > 0 {
		url += fmt.Sprintf("%slimit=%d", sep, limit)
	}
	return api.Get(ctx, url)
}

// GetGuild 获取指定频道详情。
func (api *Client) GetGuild(ctx context.Context, guildID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GuildURL, guildID))
}

// GetGuildChannels 获取频道下的子频道列表。
func (api *Client) GetGuildChannels(ctx context.Context, guildID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GuildChannelsURL, guildID))
}

// GetChannel 获取子频道详情。
func (api *Client) GetChannel(ctx context.Context, channelID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelURL, channelID))
}

// CreateGuildChannel 在频道内创建子频道（仅私域机器人）。
func (api *Client) CreateGuildChannel(ctx context.Context, guildID string, req *dto.ChannelRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GuildChannelsURL, guildID), req)
}

// UpdateGuildChannel 修改子频道信息（仅私域机器人）。
func (api *Client) UpdateGuildChannel(ctx context.Context, channelID string, req *dto.ChannelRequest) (gjson.Result, error) {
	return api.Patch(ctx, fmt.Sprintf(constant.ChannelURL, channelID), req)
}

// DeleteGuildChannel 删除子频道（仅私域机器人）。
func (api *Client) DeleteGuildChannel(ctx context.Context, channelID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.ChannelURL, channelID))
}

// CreateDirectMessageSession 创建频道私信会话。
// 发送频道私信前必须先调用此接口获取 guild_id，
// 之后使用 DMChat(guild_id, ...) 发送消息。
func (api *Client) CreateDirectMessageSession(ctx context.Context, req *dto.DirectMessageSessionRequest) (gjson.Result, error) {
	return api.Post(ctx, constant.UsersMeDMsURL, req)
}

// ── 频道成员 ────────────────────────────────────────────────────────────────

// GetChannelOnlineNums 获取子频道在线成员数。
func (api *Client) GetChannelOnlineNums(ctx context.Context, channelID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelOnlineNumsURL, channelID))
}

// GetGuildMembers 分页获取频道成员列表。
func (api *Client) GetGuildMembers(ctx context.Context, guildID, after string, limit int) (gjson.Result, error) {
	url := fmt.Sprintf(constant.GuildMembersURL+"?after=%s&limit=%d", guildID, after, limit)
	return api.Get(ctx, url)
}

// GetGuildRoleMembers 获取频道指定身份组成员列表。
func (api *Client) GetGuildRoleMembers(ctx context.Context, guildID, roleID string, startIndex, limit uint32) (gjson.Result, error) {
	url := fmt.Sprintf(constant.GuildRoleMembersURL+"?start_index=%d&limit=%d", guildID, roleID, startIndex, limit)
	return api.Get(ctx, url)
}

// GetGuildMember 获取频道单个成员详情。
func (api *Client) GetGuildMember(ctx context.Context, guildID, userID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GuildMemberURL, guildID, userID))
}

// DeleteGuildMember 删除（踢出）频道成员。
func (api *Client) DeleteGuildMember(ctx context.Context, guildID, userID string, addBlacklist bool, deleteHistoryMsgDays int) (gjson.Result, error) {
	type deleteBody struct {
		AddBlacklist         bool `json:"add_blacklist"`
		DeleteHistoryMsgDays int  `json:"delete_history_msg_days"`
	}
	// DELETE 请求需要 body，直接通过 httpclient 以 DELETE 方法发送（类似 Post helper）。
	// 必须传播错误并监听调用方 ctx。
	//
	// 此前这里直接丢弃返回值：token 未就绪时 authHeader() 会拼出
	// "QQBot "（空 token）照发不误，QQ 一律回 401，调用方拿到的是一个
	// 看不出所以然的平台错误；同时固定 30 秒的等待完全无视调用方自己的
	// deadline。
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	result, err := httpclient.Delete(fmt.Sprintf(constant.GuildMemberURL, guildID, userID)).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader()).
		SetJSON(&deleteBody{AddBlacklist: addBlacklist, DeleteHistoryMsgDays: deleteHistoryMsgDays}).
		DoJSON()
	if err != nil {
		return gjson.Result{}, err
	}
	return result, nil
}

// ── 频道身份组 ───────────────────────────────────────────────────────────────

// GetGuildRoles 获取频道身份组列表。
func (api *Client) GetGuildRoles(ctx context.Context, guildID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GuildRolesURL, guildID))
}

// CreateGuildRole 创建频道身份组。
func (api *Client) CreateGuildRole(ctx context.Context, guildID string, req *dto.GuildRoleRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GuildRolesURL, guildID), req)
}

// UpdateGuildRole 修改频道身份组。
func (api *Client) UpdateGuildRole(ctx context.Context, guildID, roleID string, req *dto.GuildRoleRequest) (gjson.Result, error) {
	return api.Patch(ctx, fmt.Sprintf(constant.GuildRoleURL, guildID, roleID), req)
}

// DeleteGuildRole 删除频道身份组。
func (api *Client) DeleteGuildRole(ctx context.Context, guildID, roleID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.GuildRoleURL, guildID, roleID))
}

// AddGuildMemberRole 向身份组添加成员。
func (api *Client) AddGuildMemberRole(ctx context.Context, guildID, userID, roleID, channelID string) (gjson.Result, error) {
	var body *dto.AddMemberRoleRequest
	if channelID != "" {
		body = &dto.AddMemberRoleRequest{Channel: &dto.ChannelRef{ID: channelID}}
	}
	return api.Put(ctx, fmt.Sprintf(constant.GuildMemberRoleURL, guildID, userID, roleID), body)
}

// DeleteGuildMemberRole 从身份组移除成员。
func (api *Client) DeleteGuildMemberRole(ctx context.Context, guildID, userID, roleID, channelID string) (gjson.Result, error) {
	// DELETE with optional body
	// 必须传播错误并监听调用方 ctx。
	//
	// 此前这里直接丢弃返回值：token 未就绪时 authHeader() 会拼出
	// "QQBot "（空 token）照发不误，QQ 一律回 401，调用方拿到的是一个
	// 看不出所以然的平台错误；同时固定 30 秒的等待完全无视调用方自己的
	// deadline。
	if err := api.tm.WaitReadyWithContext(ctx); err != nil {
		return gjson.Result{}, fmt.Errorf("qq openapi: access token 未就绪: %w", err)
	}
	req := httpclient.Delete(fmt.Sprintf(constant.GuildMemberRoleURL, guildID, userID, roleID)).
		SetContext(ctx).
		SetHeader("Authorization", api.authHeader())
	if channelID != "" {
		req = req.SetJSON(&dto.AddMemberRoleRequest{Channel: &dto.ChannelRef{ID: channelID}})
	}
	result, err := req.DoJSON()
	if err != nil {
		return gjson.Result{}, err
	}
	return result, nil
}

// GetChannelMemberPermissions 获取子频道内指定成员权限。
func (api *Client) GetChannelMemberPermissions(ctx context.Context, channelID, userID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelMemberPermURL, channelID, userID))
}

// UpdateChannelMemberPermissions 修改子频道内指定成员权限。
func (api *Client) UpdateChannelMemberPermissions(ctx context.Context, channelID, userID string, req *dto.PermissionRequest) (gjson.Result, error) {
	return api.Put(ctx, fmt.Sprintf(constant.ChannelMemberPermURL, channelID, userID), req)
}

// GetChannelRolePermissions 获取子频道内指定身份组权限。
func (api *Client) GetChannelRolePermissions(ctx context.Context, channelID, roleID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelRolePermURL, channelID, roleID))
}

// UpdateChannelRolePermissions 修改子频道内指定身份组权限。
func (api *Client) UpdateChannelRolePermissions(ctx context.Context, channelID, roleID string, req *dto.PermissionRequest) (gjson.Result, error) {
	return api.Put(ctx, fmt.Sprintf(constant.ChannelRolePermURL, channelID, roleID), req)
}

// ── 接口授权管理 ─────────────────────────────────────────────────────────────

// GetGuildAPIPermissions 获取频道已授权的接口列表。
func (api *Client) GetGuildAPIPermissions(ctx context.Context, guildID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GuildAPIPermissionsURL, guildID))
}

// RequestGuildAPIPermission 发送接口权限授权链接。
func (api *Client) RequestGuildAPIPermission(ctx context.Context, guildID string, req *dto.APIPermissionDemandRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GuildAPIPermDemandURL, guildID), req)
}

// ── 发言管理 ─────────────────────────────────────────────────────────────────

// GetGuildMessageSetting 获取频道消息频率设置。
func (api *Client) GetGuildMessageSetting(ctx context.Context, guildID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GuildMessageSettingURL, guildID))
}

// MuteGuild 频道全员禁言/解除禁言。
func (api *Client) MuteGuild(ctx context.Context, guildID string, req *dto.MuteRequest) (gjson.Result, error) {
	return api.Patch(ctx, fmt.Sprintf(constant.GuildMuteURL, guildID), req)
}

// MuteGuildMember 禁言/解除禁言频道指定成员。
func (api *Client) MuteGuildMember(ctx context.Context, guildID, userID string, req *dto.MuteRequest) (gjson.Result, error) {
	return api.Patch(ctx, fmt.Sprintf(constant.GuildMemberMuteURL, guildID, userID), req)
}

// MuteGuildMultiMembers 批量禁言/解除禁言频道成员。
func (api *Client) MuteGuildMultiMembers(ctx context.Context, guildID string, req *dto.MultipleMuteRequest) (gjson.Result, error) {
	return api.Patch(ctx, fmt.Sprintf(constant.GuildMuteURL, guildID), req)
}

// ── 内容管理：公告 ───────────────────────────────────────────────────────────

// CreateGuildAnnounce 创建频道公告。
func (api *Client) CreateGuildAnnounce(ctx context.Context, guildID string, req *dto.CreateGuildAnnounceRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GuildAnnouncesURL, guildID), req)
}

// DeleteGuildAnnounce 删除频道公告（messageID 传 "all" 可删除所有）。
func (api *Client) DeleteGuildAnnounce(ctx context.Context, guildID, messageID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.GuildAnnounceURL, guildID, messageID))
}

// ── 内容管理：精华消息 ───────────────────────────────────────────────────────

// PinMessage 添加子频道精华消息。
func (api *Client) PinMessage(ctx context.Context, channelID, messageID string) (gjson.Result, error) {
	return api.Put(ctx, fmt.Sprintf(constant.ChannelPinURL, channelID, messageID), nil)
}

// UnpinMessage 删除子频道精华消息。
func (api *Client) UnpinMessage(ctx context.Context, channelID, messageID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.ChannelPinURL, channelID, messageID))
}

// GetPinnedMessages 获取子频道所有精华消息。
func (api *Client) GetPinnedMessages(ctx context.Context, channelID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelPinsURL, channelID))
}

// ── 内容管理：日程 ───────────────────────────────────────────────────────────

// GetChannelSchedules 获取子频道日程列表。
func (api *Client) GetChannelSchedules(ctx context.Context, channelID string, since uint64) (gjson.Result, error) {
	url := fmt.Sprintf(constant.ChannelSchedulesURL, channelID)
	if since > 0 {
		url = fmt.Sprintf("%s?since=%d", url, since)
	}
	return api.Get(ctx, url)
}

// GetChannelSchedule 获取日程详情。
func (api *Client) GetChannelSchedule(ctx context.Context, channelID, scheduleID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelScheduleURL, channelID, scheduleID))
}

// CreateChannelSchedule 创建日程。
func (api *Client) CreateChannelSchedule(ctx context.Context, channelID string, req *dto.ScheduleRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.ChannelSchedulesURL, channelID), req)
}

// UpdateChannelSchedule 修改日程。
func (api *Client) UpdateChannelSchedule(ctx context.Context, channelID, scheduleID string, req *dto.ScheduleRequest) (gjson.Result, error) {
	return api.Patch(ctx, fmt.Sprintf(constant.ChannelScheduleURL, channelID, scheduleID), req)
}

// DeleteChannelSchedule 删除日程。
func (api *Client) DeleteChannelSchedule(ctx context.Context, channelID, scheduleID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.ChannelScheduleURL, channelID, scheduleID))
}

// ── 内容管理：音频 ───────────────────────────────────────────────────────────

// AudioControl 音频控制。
func (api *Client) AudioControl(ctx context.Context, channelID string, req *dto.AudioControlRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.ChannelAudioURL, channelID), req)
}

// BotOnMic 机器人上麦。
func (api *Client) BotOnMic(ctx context.Context, channelID string) (gjson.Result, error) {
	return api.Put(ctx, fmt.Sprintf(constant.ChannelMicURL, channelID), nil)
}

// BotOffMic 机器人下麦。
func (api *Client) BotOffMic(ctx context.Context, channelID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.ChannelMicURL, channelID))
}

// ── 内容管理：论坛帖子 ───────────────────────────────────────────────────────

// GetThreadList 获取子频道帖子列表。
func (api *Client) GetThreadList(ctx context.Context, channelID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelThreadsURL, channelID))
}

// GetThread 获取帖子详情。
func (api *Client) GetThread(ctx context.Context, channelID, threadID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.ChannelThreadURL, channelID, threadID))
}

// CreateThread 发表帖子。
func (api *Client) CreateThread(ctx context.Context, channelID string, req *dto.ThreadRequest) (gjson.Result, error) {
	return api.Put(ctx, fmt.Sprintf(constant.ChannelThreadsURL, channelID), req)
}

// DeleteThread 删除帖子。
func (api *Client) DeleteThread(ctx context.Context, channelID, threadID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.ChannelThreadURL, channelID, threadID))
}

// ── Gateway 接入点 ──────────────────────────────────────────────────────────

// GetGateway 获取通用 WSS 接入点（GET /gateway）。
func (api *Client) GetGateway(ctx context.Context) (gjson.Result, error) {
	return api.Get(ctx, constant.GatewayURL)
}

// GetGatewayBot 获取带分片的 WSS 接入点（GET /gateway/bot）。
func (api *Client) GetGatewayBot(ctx context.Context) (gjson.Result, error) {
	return api.Get(ctx, constant.GatewayBotURL)
}

// ── 群聊管理（2026-08 新增）────────────────────────────────────────────────

// GetGroupInfo 获取群基本信息（GET /v2/groups/{group_openid}/info）。
//
// 该接口仅白名单机器人可用。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_info.get.html
func (api *Client) GetGroupInfo(ctx context.Context, groupOpenID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GroupInfoURL, groupOpenID))
}

// GetGroupBotState 获取机器人群内状态（GET /v2/groups/{group_openid}/bot_state）。
//
// 该接口仅白名单机器人可用。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_bot_state.get.html
func (api *Client) GetGroupBotState(ctx context.Context, groupOpenID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GroupBotStateURL, groupOpenID))
}

// GetGroupJoinRequestList 拉取入群申请列表（GET /v2/groups/{group_openid}/join_request_list）。
//
// 机器人需拥有群管理员身份。cursor 为分页游标（首次传空串），limit 单页数量
// （默认 20，最大 100）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_join_request_list.get.html
func (api *Client) GetGroupJoinRequestList(ctx context.Context, groupOpenID, cursor string, limit int) (gjson.Result, error) {
	url := fmt.Sprintf(constant.GroupJoinRequestListURL, groupOpenID)
	sep := "?"
	if cursor != "" {
		url += sep + "cursor=" + cursor
		sep = "&"
	}
	if limit > 0 {
		url += fmt.Sprintf("%slimit=%d", sep, limit)
	}
	return api.Get(ctx, url)
}

// ApproveJoinRequest 审批入群申请（POST /v2/groups/{group_openid}/approval_join_request/{member_openid}）。
//
// 机器人需拥有群管理员身份。req.Op 为 approve（通过）或 decline（拒绝）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_approval_join_request_member_openid.post.html
func (api *Client) ApproveJoinRequest(ctx context.Context, groupOpenID, memberOpenID string, req *dto.ApprovalJoinRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupApprovalJoinRequestURL, groupOpenID, memberOpenID), req)
}

// GetGroupRestrictChatSetting 查询群禁言状态（GET /v2/groups/{group_openid}/restrict_chat_setting）。
//
// 机器人需拥有群管理员身份。返回全员禁言模式与成员级禁言列表。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_restrict_chat_setting.get.html
func (api *Client) GetGroupRestrictChatSetting(ctx context.Context, groupOpenID string) (gjson.Result, error) {
	return api.Get(ctx, fmt.Sprintf(constant.GroupRestrictChatSettingURL, groupOpenID))
}

// SetGroupMemberMute 设置群成员禁言（POST /v2/groups/{group_openid}/restrict_chat_setting）。
//
// 机器人需拥有群管理员身份。每项通过 op 控制增/改/删，单次设置不能超过 10 个。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_restrict_chat_setting.post.html
func (api *Client) SetGroupMemberMute(ctx context.Context, groupOpenID string, req *dto.SetRestrictChatSettingRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.GroupRestrictChatSettingURL, groupOpenID), req)
}

// GetJoinApprovalStrategyList 查询入群自动审批策略列表（GET /v2/groups/join_approval_strategy）。
//
// cursor 为分页游标（首次传空串），limit 单页数量（默认 20，最大 100）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_join_approval_strategy.get.html
func (api *Client) GetJoinApprovalStrategyList(ctx context.Context, cursor string, limit int) (gjson.Result, error) {
	url := constant.JoinApprovalStrategyURL
	sep := "?"
	if cursor != "" {
		url += sep + "cursor=" + cursor
		sep = "&"
	}
	if limit > 0 {
		url += fmt.Sprintf("%slimit=%d", sep, limit)
	}
	return api.Get(ctx, url)
}

// CreateJoinApprovalStrategy 创建入群自动审批策略（POST /v2/groups/join_approval_strategy）。
//
// 一个机器人最多 20 个策略。设置的规则只有当机器人拥有群管理员身份时才会生效运行。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_join_approval_strategy.post.html
func (api *Client) CreateJoinApprovalStrategy(ctx context.Context, req *dto.CreateJoinApprovalStrategyRequest) (gjson.Result, error) {
	return api.Post(ctx, constant.JoinApprovalStrategyURL, req)
}

// UpdateJoinApprovalStrategy 修改入群自动审批策略（PATCH /v2/groups/join_approval_strategy/{strategy_id}）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_join_approval_strategy_strategy_id.patch.html
func (api *Client) UpdateJoinApprovalStrategy(ctx context.Context, strategyID string, req *dto.UpdateJoinApprovalStrategyRequest) (gjson.Result, error) {
	return api.Patch(ctx, fmt.Sprintf(constant.JoinApprovalStrategyIDURL, strategyID), req)
}

// DeleteJoinApprovalStrategy 删除入群自动审批策略（DELETE /v2/groups/join_approval_strategy/{strategy_id}）。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_join_approval_strategy_strategy_id.delete.html
func (api *Client) DeleteJoinApprovalStrategy(ctx context.Context, strategyID string) (gjson.Result, error) {
	return api.Delete(ctx, fmt.Sprintf(constant.JoinApprovalStrategyIDURL, strategyID))
}

// ExecuteJoinApprovalStrategy 执行入群自动审批策略（POST /v2/groups/join_approval_strategy/{strategy_id}/execute）。
//
// 对策略关联的全部群发起全量扫描，命中白名单号码的入群申请自动审批通过。异步执行，约 10 分钟完成。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_join_approval_strategy_strategy_id_execute.post.html
func (api *Client) ExecuteJoinApprovalStrategy(ctx context.Context, strategyID string) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.JoinApprovalStrategyExecuteURL, strategyID), struct{}{})
}

// UpdateJoinApprovalStrategyWhitelist 修改入群自动审批策略的白名单号码（POST /v2/groups/join_approval_strategy/{strategy_id}/whitelist_users）。
//
// 单次最多 10000 个，号码上限 10W。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_join_approval_strategy_strategy_id_whitelist_users.post.html
func (api *Client) UpdateJoinApprovalStrategyWhitelist(ctx context.Context, strategyID string, req *dto.UpdateWhitelistUsersRequest) (gjson.Result, error) {
	return api.Post(ctx, fmt.Sprintf(constant.JoinApprovalStrategyWhitelistURL, strategyID), req)
}

// ── 机器人自身管理 ──────────────────────────────────────────────────────────

// GenerateURLLink 生成机器人分享链接（POST /v2/generate_url_link）。
//
// 用于邀请用户添加机器人为好友；用户通过链接添加机器人时，
// req.CallbackData（最长 32 字符）会透传给开发者。
//
// https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_generate_url_link.post.html
func (api *Client) GenerateURLLink(ctx context.Context, req *dto.GenerateURLLinkRequest) (gjson.Result, error) {
	return api.Post(ctx, constant.GenerateURLLinkURL, req)
}
