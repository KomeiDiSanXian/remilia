package onebot

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// Sender
// ────────────────────────────────────────────────────────────────────────────

// Sender 实现了以下 platform 接口：
//   - platform.Sender
//   - platform.SessionNotifier (NotifyUser / NotifyGroup 主动推送)
//   - platform.MessageDeleter  (delete_msg)
//   - platform.GroupManager    (kick / ban / set_admin)
//   - platform.GroupSettings   (群名称/名片/头衔/退群)
//   - platform.InvitationHandler (好友/群请求)
//   - platform.AutoModerator   (全体禁言)
//   - platform.MessageHistoryProvider (群/私聊历史消息)
//   - platform.AnnouncementManager (群公告)
//   - platform.APIProvider     (PlatformAPI 返回自身，暴露全部 OneBot 扩展动作)
//
// Sender 是 OneBot 扩展动作的完整门面：通过 platform.GetPlatformAPI 获取后
// 即可调用本类型上的全部方法（合并转发、历史消息、群文件、闪传、AI 角色等
// 160+ 个 OneBot 动作）。跨协议端动作名差异（如 NapCat 的 set_group_sign vs
// LLB 的 send_group_sign）由 404 自动回退链处理，调用方无需区分协议端。
type Sender struct {
	api APIClient
}

// newSender 创建一个由指定 APIClient 驱动的 Sender。
func newSender(api APIClient) *Sender {
	return &Sender{api: api}
}

// PlatformAPI 实现 platform.APIProvider，返回 Sender 自身。
//
// 调用示例（在事件处理中）：
//
//	api, ok := platform.GetPlatformAPI(ctx.GetPlatformSender()).(*onebot.Sender)
//	if ok {
//	    nodes := onebot.MessageChain{node}
//	    _, _ = api.SendGroupForwardMsg(ctx, 123456, nodes)
//	}
func (s *Sender) PlatformAPI() any { return s }

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupSettings / MessageHistoryProvider / AnnouncementManager
// ────────────────────────────────────────────────────────────────────────────

// GetGroupHistory 实现 platform.MessageHistoryProvider，
// 获取群历史消息（从新到旧）。
func (s *Sender) GetGroupHistory(ctx stdctx.Context, chatID string, limit int) ([]platform.Message, error) {
	groupID, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("onebot: invalid group_id %q: %w", chatID, err)
	}
	res, err := s.GetGroupMsgHistory(ctx, groupID, 0, limit)
	if err != nil {
		return nil, err
	}
	return historyToMessages(res.Messages, true), nil
}

// GetFriendHistory 实现 platform.MessageHistoryProvider，
// 获取好友（私聊）历史消息（从新到旧）。
func (s *Sender) GetFriendHistory(ctx stdctx.Context, chatID string, limit int) ([]platform.Message, error) {
	userID, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("onebot: invalid user_id %q: %w", chatID, err)
	}
	res, err := s.GetFriendMsgHistory(ctx, userID, 0, limit)
	if err != nil {
		return nil, err
	}
	return historyToMessages(res.Messages, false), nil
}

// historyToMessages 将 OneBot 历史消息转换为平台无关消息快照。
func historyToMessages(msgs []HistoryMsg, isGroup bool) []platform.Message {
	out := make([]platform.Message, 0, len(msgs))
	for _, m := range msgs {
		var chat platform.ChatInfo
		if isGroup {
			chat = platform.ChatInfo{ID: strconv.FormatInt(m.GroupID, 10), IsGroup: true}
		} else {
			chat = platform.ChatInfo{ID: strconv.FormatInt(m.UserID, 10)}
		}
		out = append(out, platform.Message{
			ID:        strconv.FormatInt(int64(m.MessageID), 10),
			Sender:    platform.UserInfo{ID: strconv.FormatInt(m.UserID, 10)},
			Chat:      chat,
			Content:   m.Message.FullText(),
			Timestamp: time.Unix(m.Time, 0),
		})
	}
	return out
}

// SendAnnouncement 实现 platform.AnnouncementManager，发布群公告。
func (s *Sender) SendAnnouncement(ctx stdctx.Context, groupID, content, imageURL string) error {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot: invalid group_id %q: %w", groupID, err)
	}
	return s.SendGroupNotice(ctx, gid, content, imageURL)
}

// GetAnnouncements 实现 platform.AnnouncementManager，获取群公告列表。
func (s *Sender) GetAnnouncements(ctx stdctx.Context, groupID string) ([]platform.Announcement, error) {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("onebot: invalid group_id %q: %w", groupID, err)
	}
	notices, err := s.GetGroupNotice(ctx, gid)
	if err != nil {
		return nil, err
	}
	out := make([]platform.Announcement, 0, len(notices))
	for _, n := range notices {
		out = append(out, platform.Announcement{
			ID:          n.NoticeID,
			Content:     n.Content,
			PublisherID: strconv.FormatInt(n.UserID, 10),
			Timestamp:   n.Time,
		})
	}
	return out, nil
}

// 编译期接口实现检查。
var _ platform.APIProvider = (*Sender)(nil)
var _ platform.GroupSettings = (*Sender)(nil)
var _ platform.MessageHistoryProvider = (*Sender)(nil)
var _ platform.AnnouncementManager = (*Sender)(nil)

// ────────────────────────────────────────────────────────────────────────────
// platform.Sender
// ────────────────────────────────────────────────────────────────────────────

// Send 通过 OneBot API 发送 OutboundMessage。
//
// 路由规则：
//   - req.Target.IsGroup == true          → send_group_msg
//   - req.Target.IsGroup == false (IsDM)  → send_private_msg
//
// 消息链由 req.Message 构建，若设置了回复和 @ 则一并包含。
func (s *Sender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if req.Target.ID == "" {
		return platform.SendResult{}, fmt.Errorf("onebot sender: target ID is empty")
	}

	chain := OutboundToChain(req.Message)

	var (
		result platform.SendResult
		err    error
	)

	if req.Target.IsGroup {
		result, err = s.sendGroup(ctx, req.Target, chain)
	} else {
		result, err = s.sendPrivate(ctx, req.Target, chain)
	}

	return result, err
}

func (s *Sender) sendGroup(ctx stdctx.Context, target platform.ChatInfo, chain MessageChain) (platform.SendResult, error) {
	groupID, err := strconv.ParseInt(target.ID, 10, 64)
	if err != nil {
		return platform.SendResult{}, fmt.Errorf("onebot sender: invalid group_id %q: %w", target.ID, err)
	}

	params := SendGroupMsgParams{
		GroupID: groupID,
		Message: chain,
	}

	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "send_group_msg", params, &res); err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, PlatformID, target.ID,
			err.Error(), 0, err,
		)
	}

	return platform.SendResult{
		Platform:  PlatformID,
		MessageID: strconv.FormatInt(int64(res.MessageID), 10),
		Timestamp: time.Now(),
	}, nil
}

func (s *Sender) sendPrivate(ctx stdctx.Context, target platform.ChatInfo, chain MessageChain) (platform.SendResult, error) {
	userID, err := strconv.ParseInt(target.ID, 10, 64)
	if err != nil {
		return platform.SendResult{}, fmt.Errorf("onebot sender: invalid user_id %q: %w", target.ID, err)
	}

	params := SendPrivateMsgParams{
		UserID:  userID,
		Message: chain,
	}

	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "send_private_msg", params, &res); err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, PlatformID, target.ID,
			err.Error(), 0, err,
		)
	}

	return platform.SendResult{
		Platform:  PlatformID,
		MessageID: strconv.FormatInt(int64(res.MessageID), 10),
		Timestamp: time.Now(),
	}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// platform.SessionNotifier
// ────────────────────────────────────────────────────────────────────────────

// NotifyUser 向指定用户发送私聊消息，实现 platform.SessionNotifier。
//
// 与 Send 不同，此方法无需事件上下文，直接凭 userID 主动推送。
func (s *Sender) NotifyUser(ctx stdctx.Context, userID string, msg platform.OutboundMessage) error {
	_, err := s.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: userID, IsGroup: false},
		Message: msg,
	})
	return err
}

// NotifyGroup 向指定群组发送消息，实现 platform.SessionNotifier。
//
// 机器人需已加入该群，否则平台将返回错误。
func (s *Sender) NotifyGroup(ctx stdctx.Context, groupID string, msg platform.OutboundMessage) error {
	_, err := s.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: groupID, IsGroup: true},
		Message: msg,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// platform.MessageDeleter
// ────────────────────────────────────────────────────────────────────────────

// Delete 通过调用 delete_msg 实现 platform.MessageDeleter.
//
// chatID 参数被忽略（OneBot delete_msg 只需消息 ID）。
func (s *Sender) Delete(ctx stdctx.Context, _ string, messageID string) error {
	msgID, err := strconv.ParseInt(messageID, 10, 32)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid message_id %q: %w", messageID, err)
	}
	_, err = s.api.Call(ctx, "delete_msg", MessageIDParams{MessageID: msgID})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupManager
// ────────────────────────────────────────────────────────────────────────────

// KickMember 通过调用 set_group_kick 实现 platform.GroupManager。
func (s *Sender) KickMember(ctx stdctx.Context, groupID, userID string, permanent bool) error {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid group_id: %w", err)
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid user_id: %w", err)
	}
	_, err = s.api.Call(ctx, "set_group_kick", SetGroupKickParams{
		GroupID:          gid,
		UserID:           uid,
		RejectAddRequest: permanent,
	})
	return err
}

// BanMember 通过调用 set_group_ban 实现 platform.GroupManager。
// duration == 0 表示解除禁言。
func (s *Sender) BanMember(ctx stdctx.Context, groupID, userID string, duration time.Duration) error {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid group_id: %w", err)
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid user_id: %w", err)
	}
	_, err = s.api.Call(ctx, "set_group_ban", SetGroupBanParams{
		GroupID:  gid,
		UserID:   uid,
		Duration: int64(duration.Seconds()),
	})
	return err
}

// SetAdmin 通过调用 set_group_admin 实现 platform.GroupManager。
func (s *Sender) SetAdmin(ctx stdctx.Context, groupID, userID string, isAdmin bool) error {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid group_id: %w", err)
	}
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid user_id: %w", err)
	}
	_, err = s.api.Call(ctx, "set_group_admin", SetGroupAdminParams{
		GroupID: gid,
		UserID:  uid,
		Enable:  isAdmin,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// platform.InvitationHandler
// ────────────────────────────────────────────────────────────────────────────

// AcceptGroupInvite 实现 platform.InvitationHandler。
// inviteID 应为群请求事件中的 flag 字段。
func (s *Sender) AcceptGroupInvite(ctx stdctx.Context, inviteID string) error {
	_, err := s.api.Call(ctx, "set_group_add_request", SetGroupAddRequestParams{
		Flag:    inviteID,
		SubType: GroupRequestInvite,
		Approve: true,
	})
	return err
}

// RejectGroupInvite 实现 platform.InvitationHandler。
func (s *Sender) RejectGroupInvite(ctx stdctx.Context, inviteID, reason string) error {
	_, err := s.api.Call(ctx, "set_group_add_request", SetGroupAddRequestParams{
		Flag:    inviteID,
		SubType: GroupRequestInvite,
		Approve: false,
		Reason:  reason,
	})
	return err
}

// AcceptFriendRequest 实现 platform.InvitationHandler。
func (s *Sender) AcceptFriendRequest(ctx stdctx.Context, requestID string) error {
	_, err := s.api.Call(ctx, "set_friend_add_request", SetFriendAddRequestParams{
		Flag:    requestID,
		Approve: true,
	})
	return err
}

// RejectFriendRequest 实现 platform.InvitationHandler。
func (s *Sender) RejectFriendRequest(ctx stdctx.Context, requestID, _ string) error {
	_, err := s.api.Call(ctx, "set_friend_add_request", SetFriendAddRequestParams{
		Flag:    requestID,
		Approve: false,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// platform.AutoModerator
// ────────────────────────────────────────────────────────────────────────────

// DeleteMemberMessage 实现 platform.AutoModerator。
//
// OneBot V11 所有消息均使用同一个 delete_msg 接口。
func (s *Sender) DeleteMemberMessage(ctx stdctx.Context, _ string, messageID string) error {
	return s.Delete(ctx, "", messageID)
}

// MuteAll 通过调用 set_group_whole_ban 实现 platform.AutoModerator。
func (s *Sender) MuteAll(ctx stdctx.Context, groupID string, mute bool) error {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid group_id: %w", err)
	}
	_, err = s.api.Call(ctx, "set_group_whole_ban", GroupToggleParams{
		GroupID: gid,
		Enable:  mute,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 消息相关扩展接口
// ────────────────────────────────────────────────────────────────────────────

// GetMsg 获取指定消息的详细信息。
func (s *Sender) GetMsg(ctx stdctx.Context, messageID int64) (*GetMsgResult, error) {
	var result GetMsgResult
	if err := callDecoded(ctx, s.api, "get_msg", MessageIDParams{MessageID: messageID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetForwardMsg 获取合并转发消息的内容。
func (s *Sender) GetForwardMsg(ctx stdctx.Context, id string) (*GetForwardMsgResult, error) {
	var result GetForwardMsgResult
	if err := callDecoded(ctx, s.api, "get_forward_msg", GetForwardMsgParams{ID: id}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendLike 给指定用户发送好友赞。
// times 为点赞次数（上限 10），传 0 时使用默认值 1。
func (s *Sender) SendLike(ctx stdctx.Context, userID int64, times int) error {
	_, err := s.api.Call(ctx, "send_like", SendLikeParams{
		UserID: userID,
		Times:  times,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 群管理扩展接口（不属于 platform 接口）
// ────────────────────────────────────────────────────────────────────────────

// SetGroupCard 设置群成员的群名片（备注）。
func (s *Sender) SetGroupCard(ctx stdctx.Context, groupID, userID, card string) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	uid, _ := strconv.ParseInt(userID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_card", SetGroupCardParams{
		GroupID: gid,
		UserID:  uid,
		Card:    card,
	})
	return err
}

// SetGroupName 修改群名称。
func (s *Sender) SetGroupName(ctx stdctx.Context, groupID, name string) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_name", SetGroupNameParams{
		GroupID:   gid,
		GroupName: name,
	})
	return err
}

// LeaveGroup 退出（或解散）群组。
func (s *Sender) LeaveGroup(ctx stdctx.Context, groupID string, dismiss bool) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_leave", SetGroupLeaveParams{
		GroupID:   gid,
		IsDismiss: dismiss,
	})
	return err
}

// SetGroupSpecialTitle 设置或移除群成员的专属头衔。
func (s *Sender) SetGroupSpecialTitle(ctx stdctx.Context, groupID, userID, title string) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	uid, _ := strconv.ParseInt(userID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_special_title", SetGroupSpecialTitleParams{
		GroupID:      gid,
		UserID:       uid,
		SpecialTitle: title,
		Duration:     -1, // 永久
	})
	return err
}

// BanAnonymous 禁言群内匿名用户。
func (s *Sender) BanAnonymous(ctx stdctx.Context, groupID string, anon *AnonymousInfo, duration time.Duration) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_anonymous_ban", SetGroupAnonymousBanParams{
		GroupID:   gid,
		Anonymous: anon,
		Duration:  int64(duration.Seconds()),
	})
	return err
}

// SetGroupAnonymous 开启或关闭群匿名发言功能。
func (s *Sender) SetGroupAnonymous(ctx stdctx.Context, groupID string, enable bool) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_anonymous", GroupToggleParams{
		GroupID: gid,
		Enable:  enable,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 账号信息查询接口
// ────────────────────────────────────────────────────────────────────────────

// GetLoginInfo 获取机器人自身的 QQ 号和昵称。
func (s *Sender) GetLoginInfo(ctx stdctx.Context) (*GetLoginInfoResult, error) {
	var result GetLoginInfoResult
	if err := callDecoded(ctx, s.api, "get_login_info", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetStrangerInfo 获取陌生人信息。
// noCache 为 true 时不使用缓存，直接向服务器请求。
func (s *Sender) GetStrangerInfo(ctx stdctx.Context, userID int64, noCache bool) (*StrangerInfo, error) {
	var result StrangerInfo
	if err := callDecoded(ctx, s.api, "get_stranger_info", GetStrangerInfoParams{
		UserID:  userID,
		NoCache: noCache,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFriendList 获取好友列表。
func (s *Sender) GetFriendList(ctx stdctx.Context) ([]FriendInfo, error) {
	var result []FriendInfo
	if err := callDecoded(ctx, s.api, "get_friend_list", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupInfo 获取群信息。
// noCache 为 true 时不使用缓存，直接向服务器请求。
func (s *Sender) GetGroupInfo(ctx stdctx.Context, groupID int64, noCache bool) (*GroupInfo, error) {
	var result GroupInfo
	if err := callDecoded(ctx, s.api, "get_group_info", GetGroupInfoParams{
		GroupID: groupID,
		NoCache: noCache,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGroupList 获取已加入的群列表。
func (s *Sender) GetGroupList(ctx stdctx.Context) ([]GroupInfo, error) {
	var result []GroupInfo
	if err := callDecoded(ctx, s.api, "get_group_list", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupMemberInfo 获取群成员信息。
// noCache 为 true 时不使用缓存，直接向服务器请求。
func (s *Sender) GetGroupMemberInfo(ctx stdctx.Context, groupID, userID int64, noCache bool) (*GroupMemberInfo, error) {
	var result GroupMemberInfo
	if err := callDecoded(ctx, s.api, "get_group_member_info", GetGroupMemberInfoParams{
		GroupID: groupID,
		UserID:  userID,
		NoCache: noCache,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGroupMemberList 获取群成员列表。
func (s *Sender) GetGroupMemberList(ctx stdctx.Context, groupID int64) ([]GroupMemberInfo, error) {
	var result []GroupMemberInfo
	if err := callDecoded(ctx, s.api, "get_group_member_list", GroupIDParams{
		GroupID: groupID,
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupAtAllRemain 查询群 @全体成员 剩余次数。
func (s *Sender) GetGroupAtAllRemain(ctx stdctx.Context, groupID int64) (*GetGroupAtAllRemainResult, error) {
	var result GetGroupAtAllRemainResult
	if err := callDecoded(ctx, s.api, "get_group_at_all_remain", GroupIDParams{
		GroupID: groupID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGroupHonorInfo 获取群荣誉信息。
// honorType 可为 "talkative"、"performer"、"legend"、"strong_newbie"、"emotion" 或 "all"。
func (s *Sender) GetGroupHonorInfo(ctx stdctx.Context, groupID int64, honorType string) (*GroupHonorInfo, error) {
	var result GroupHonorInfo
	if err := callDecoded(ctx, s.api, "get_group_honor_info", GetGroupHonorInfoParams{
		GroupID: groupID,
		Type:    honorType,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 凭证与文件接口
// ────────────────────────────────────────────────────────────────────────────

// GetCookies 获取 Cookies。
func (s *Sender) GetCookies(ctx stdctx.Context, domain string) (*GetCookiesResult, error) {
	var result GetCookiesResult
	if err := callDecoded(ctx, s.api, "get_cookies", GetCookiesParams{Domain: domain}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetCSRFToken 获取 CSRF Token。
func (s *Sender) GetCSRFToken(ctx stdctx.Context) (*GetCSRFTokenResult, error) {
	var result GetCSRFTokenResult
	if err := callDecoded(ctx, s.api, "get_csrf_token", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetCredentials 获取 QQ 相关接口凭证（Cookies + CSRF Token 的合并版本）。
func (s *Sender) GetCredentials(ctx stdctx.Context, domain string) (*GetCredentialsResult, error) {
	var result GetCredentialsResult
	if err := callDecoded(ctx, s.api, "get_credentials", GetCredentialsParams{Domain: domain}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetRecord 获取语音文件，转换为指定格式后返回本地路径。
// outFormat 可为 mp3、amr、wma、m4a、spx、ogg、wav、flac。
func (s *Sender) GetRecord(ctx stdctx.Context, file, outFormat string) (*GetRecordResult, error) {
	var result GetRecordResult
	if err := callDecoded(ctx, s.api, "get_record", GetRecordParams{
		File:      file,
		OutFormat: outFormat,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetImage 获取图片文件的本地路径。
func (s *Sender) GetImage(ctx stdctx.Context, file string) (*GetImageResult, error) {
	var result GetImageResult
	if err := callDecoded(ctx, s.api, "get_image", GetImageParams{File: file}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 能力检测接口
// ────────────────────────────────────────────────────────────────────────────

// CanSendImage 检查当前账号是否可以发送图片。
func (s *Sender) CanSendImage(ctx stdctx.Context) (bool, error) {
	var result CanSendResult
	if err := callDecoded(ctx, s.api, "can_send_image", struct{}{}, &result); err != nil {
		return false, err
	}
	return result.Yes, nil
}

// CanSendRecord 检查当前账号是否可以发送语音。
func (s *Sender) CanSendRecord(ctx stdctx.Context) (bool, error) {
	var result CanSendResult
	if err := callDecoded(ctx, s.api, "can_send_record", struct{}{}, &result); err != nil {
		return false, err
	}
	return result.Yes, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 运行状态接口
// ────────────────────────────────────────────────────────────────────────────

// GetStatus 获取 OneBot 实现的当前运行状态。
func (s *Sender) GetStatus(ctx stdctx.Context) (*GetStatusResult, error) {
	var result GetStatusResult
	if err := callDecoded(ctx, s.api, "get_status", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetVersionInfo 获取 OneBot 实现的版本信息。
func (s *Sender) GetVersionInfo(ctx stdctx.Context) (*GetVersionInfoResult, error) {
	var result GetVersionInfoResult
	if err := callDecoded(ctx, s.api, "get_version_info", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetRestart 重启 OneBot 实现。
// delay 为重启前的等待毫秒数，传 0 时立即重启。
func (s *Sender) SetRestart(ctx stdctx.Context, delay int) error {
	_, err := s.api.Call(ctx, "set_restart", SetRestartParams{Delay: delay})
	return err
}

// CleanCache 清理 OneBot 实现的本地缓存（如图片、语音等）。
func (s *Sender) CleanCache(ctx stdctx.Context) error {
	_, err := s.api.Call(ctx, "clean_cache", struct{}{})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 扩展动作：消息收发与检索（2026-08 对照 OneBot 11 标准、go-cqhttp、
// LLOneBot/LuckyLilliaBot、NapCat、Lagrange.OneBot v1 补齐）
// ────────────────────────────────────────────────────────────────────────────

// SendMsg 通过 send_msg 智能路由发送消息（按 message_type 或目标自动判断）。
func (s *Sender) SendMsg(ctx stdctx.Context, params SendMsgParams) (*SendMsgResult, error) {
	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "send_msg", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SendForwardMsg 发送合并转发消息（智能路由，go-cqhttp/LLB 扩展）。
func (s *Sender) SendForwardMsg(ctx stdctx.Context, params SendForwardMsgParams) (*SendMsgResult, error) {
	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "send_forward_msg", params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SendGroupForwardMsg 向群发送合并转发消息（OneBot 11 标准）。
// nodes 为 node 消息段数组（可用 NewNodeSegment 构造）。
func (s *Sender) SendGroupForwardMsg(ctx stdctx.Context, groupID int64, nodes MessageChain) (*SendMsgResult, error) {
	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "send_group_forward_msg", SendGroupForwardMsgParams{
		GroupID:  groupID,
		Messages: nodes,
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SendPrivateForwardMsg 向好友发送合并转发消息（OneBot 11 标准）。
// nodes 为 node 消息段数组（可用 NewNodeSegment 构造）。
func (s *Sender) SendPrivateForwardMsg(ctx stdctx.Context, userID int64, nodes MessageChain) (*SendMsgResult, error) {
	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "send_private_forward_msg", SendPrivateForwardMsgParams{
		UserID:   userID,
		Messages: nodes,
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetGroupMsgHistory 获取群历史消息（go-cqhttp 定义）。
// messageSeq 为起始消息序号（0 从最新开始），count 为数量（0 用默认 20）。
func (s *Sender) GetGroupMsgHistory(ctx stdctx.Context, groupID int64, messageSeq int64, count int) (*GetMsgHistoryResult, error) {
	var result GetMsgHistoryResult
	if err := callDecoded(ctx, s.api, "get_group_msg_history", GetGroupMsgHistoryParams{
		GroupID:    groupID,
		MessageSeq: messageSeq,
		Count:      count,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFriendMsgHistory 获取好友历史消息（go-cqhttp 定义）。
func (s *Sender) GetFriendMsgHistory(ctx stdctx.Context, userID int64, messageSeq int64, count int) (*GetMsgHistoryResult, error) {
	var result GetMsgHistoryResult
	if err := callDecoded(ctx, s.api, "get_friend_msg_history", GetFriendMsgHistoryParams{
		UserID:     userID,
		MessageSeq: messageSeq,
		Count:      count,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetEssenceMsgList 获取群精华消息列表。
func (s *Sender) GetEssenceMsgList(ctx stdctx.Context, groupID int64) ([]EssenceMsg, error) {
	var result []EssenceMsg
	if err := callDecoded(ctx, s.api, "get_essence_msg_list", GetGroupInfoParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetEssenceMsg 将消息设为群精华。
func (s *Sender) SetEssenceMsg(ctx stdctx.Context, messageID int64) error {
	_, err := s.api.Call(ctx, "set_essence_msg", MessageIDParams{MessageID: messageID})
	return err
}

// DeleteEssenceMsg 取消群精华消息。
func (s *Sender) DeleteEssenceMsg(ctx stdctx.Context, messageID int64) error {
	_, err := s.api.Call(ctx, "delete_essence_msg", MessageIDParams{MessageID: messageID})
	return err
}

// MarkMsgAsRead 将消息标记为已读。
func (s *Sender) MarkMsgAsRead(ctx stdctx.Context, messageID int64) error {
	_, err := s.api.Call(ctx, "mark_msg_as_read", MessageIDParams{MessageID: messageID})
	return err
}

// UploadGroupFile 向群上传文件（file 支持本地路径或 URL）。
func (s *Sender) UploadGroupFile(ctx stdctx.Context, groupID int64, file, name, folder string) error {
	_, err := s.api.Call(ctx, "upload_group_file", UploadGroupFileParams{
		GroupID: groupID,
		File:    file,
		Name:    name,
		Folder:  folder,
	})
	return err
}

// UploadPrivateFile 向好友发送文件（file 支持本地路径或 URL）。
func (s *Sender) UploadPrivateFile(ctx stdctx.Context, userID int64, file, name string) error {
	_, err := s.api.Call(ctx, "upload_private_file", UploadPrivateFileParams{
		UserID: userID,
		File:   file,
		Name:   name,
	})
	return err
}

// DownloadFile 下载文件到 OneBot 实现的本地缓存，返回本地路径。
func (s *Sender) DownloadFile(ctx stdctx.Context, url string, threadCount int, headers map[string]string, timeout int) (*DownloadFileResult, error) {
	var result DownloadFileResult
	if err := callDecoded(ctx, s.api, "download_file", DownloadFileParams{
		URL:         url,
		ThreadCount: threadCount,
		Headers:     headers,
		Timeout:     timeout,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFile 根据文件 ID 获取文件信息（本地路径与 URL，go-cqhttp/LLB 扩展）。
func (s *Sender) GetFile(ctx stdctx.Context, fileID string) (*GetFileResult, error) {
	var result GetFileResult
	if err := callDecoded(ctx, s.api, "get_file", GetFileParams{FileID: fileID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// OCRImage 识别图片中的文字（image 为图片路径或 URL）。
func (s *Sender) OCRImage(ctx stdctx.Context, image string) (*OCRImageResult, error) {
	var result OCRImageResult
	if err := callDecoded(ctx, s.api, "ocr_image", OCRImageParams{Image: image}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteFriend 删除好友。
func (s *Sender) DeleteFriend(ctx stdctx.Context, userID int64) error {
	_, err := s.api.Call(ctx, "delete_friend", UserIDParams{UserID: userID})
	return err
}

// SendGroupNotice 发布群公告。
// image 为可选配图 URL，留空表示纯文字公告。
//
// 自动兼容协议端动作名差异：NapCat/go-cqhttp 用公开名 send_group_notice，
// LLOneBot/LuckyLilliaBot 用隐藏名 _send_group_notice（404 时自动回退）。
func (s *Sender) SendGroupNotice(ctx stdctx.Context, groupID int64, content, image string) error {
	_, err := callCompat(ctx, s.api, "send_group_notice", []string{"_send_group_notice"}, SendGroupNoticeParams{
		GroupID: groupID,
		Content: content,
		Image:   image,
	})
	return err
}

// GetGroupNotice 获取群公告列表。
//
// 自动兼容协议端动作名差异：NapCat/go-cqhttp 用公开名 get_group_notice，
// LLOneBot/LuckyLilliaBot 用隐藏名 _get_group_notice（404 时自动回退）。
func (s *Sender) GetGroupNotice(ctx stdctx.Context, groupID int64) ([]GroupNotice, error) {
	var result GetGroupNoticeResult
	if err := callCompatDecoded(ctx, s.api, "get_group_notice", []string{"_get_group_notice"}, GroupIDParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return result.Notices, nil
}

// SetGroupPortrait 修改群头像（file 支持本地路径或 URL）。
func (s *Sender) SetGroupPortrait(ctx stdctx.Context, groupID int64, file string) error {
	_, err := s.api.Call(ctx, "set_group_portrait", SetGroupPortraitParams{
		GroupID: groupID,
		File:    file,
	})
	return err
}

// GetGroupSystemMsg 获取群系统消息（入群/邀请请求，go-cqhttp/LLB 扩展）。
func (s *Sender) GetGroupSystemMsg(ctx stdctx.Context) (*GroupSystemMsg, error) {
	var result GroupSystemMsg
	if err := callDecoded(ctx, s.api, "get_group_system_msg", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGuildList 获取已加入的频道列表（go-cqhttp/LLB 扩展）。
func (s *Sender) GetGuildList(ctx stdctx.Context) ([]GuildInfo, error) {
	var result []GuildInfo
	if err := callDecoded(ctx, s.api, "get_guild_list", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SendGroupSign 群签到。
// 自动兼容协议端动作名差异：LLB 用 send_group_sign，NapCat 用 set_group_sign。
func (s *Sender) SendGroupSign(ctx stdctx.Context, groupID int64) error {
	_, err := callCompat(ctx, s.api, "send_group_sign", []string{"set_group_sign"}, GroupIDParams{GroupID: groupID})
	return err
}

// SendPoke 发送戳一戳（OneBot 11 标准扩展）。
// targetID 为被戳的 QQ 号；groupID 与 userID 至少填一个以确定会话。
func (s *Sender) SendPoke(ctx stdctx.Context, groupID, userID, targetID int64) error {
	_, err := s.api.Call(ctx, "send_poke", SendPokeParams{
		UserID:   userID,
		GroupID:  groupID,
		TargetID: targetID,
	})
	return err
}

// FriendPoke 戳好友（LLB 扩展，等价于 send_poke 的私聊形式）。
func (s *Sender) FriendPoke(ctx stdctx.Context, userID int64) error {
	_, err := s.api.Call(ctx, "friend_poke", UserIDParams{UserID: userID})
	return err
}

// GroupPoke 在群内戳成员（LLB 扩展）。
func (s *Sender) GroupPoke(ctx stdctx.Context, groupID, userID int64) error {
	_, err := s.api.Call(ctx, "group_poke", GroupPokeParams{
		GroupID: groupID,
		UserID:  userID,
	})
	return err
}

// SetMsgEmojiLike 为消息添加表情回应（LLB 扩展）。
func (s *Sender) SetMsgEmojiLike(ctx stdctx.Context, messageID int64, emojiID string) error {
	_, err := s.api.Call(ctx, "set_msg_emoji_like", SetMsgEmojiLikeParams{
		MessageID: messageID,
		EmojiID:   emojiID,
	})
	return err
}

// UnsetMsgEmojiLike 取消消息的表情回应（LLB 扩展）。
func (s *Sender) UnsetMsgEmojiLike(ctx stdctx.Context, messageID int64, emojiID string) error {
	_, err := s.api.Call(ctx, "unset_msg_emoji_like", SetMsgEmojiLikeParams{
		MessageID: messageID,
		EmojiID:   emojiID,
	})
	return err
}

// FetchEmojiLike 获取消息的表情回应列表（LLB 扩展）。
func (s *Sender) FetchEmojiLike(ctx stdctx.Context, messageID int64) (*FetchEmojiLikeResult, error) {
	var result FetchEmojiLikeResult
	if err := callDecoded(ctx, s.api, "fetch_emoji_like", MessageIDParams{MessageID: messageID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetGroupReaction 为群消息添加/取消表情回应（Lagrange.OneBot v1 扩展）。
func (s *Sender) SetGroupReaction(ctx stdctx.Context, groupID int64, messageID int64, code string, isAdd bool) error {
	_, err := s.api.Call(ctx, "set_group_reaction", SetGroupReactionParams{
		GroupID:   groupID,
		MessageID: messageID,
		Code:      code,
		IsAdd:     isAdd,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 扩展动作：账号资料（LLOneBot/LuckyLilliaBot 扩展）
// ────────────────────────────────────────────────────────────────────────────

// SetQQAvatar 修改机器人 QQ 头像（file 支持本地路径或 URL）。
func (s *Sender) SetQQAvatar(ctx stdctx.Context, file string) error {
	_, err := s.api.Call(ctx, "set_qq_avatar", FilePathParams{File: file})
	return err
}

// GetQQAvatar 获取指定用户 QQ 头像 URL。
func (s *Sender) GetQQAvatar(ctx stdctx.Context, userID int64) (*URLResult, error) {
	var result URLResult
	if err := callDecoded(ctx, s.api, "get_qq_avatar", UserIDParams{UserID: userID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetQQProfile 修改机器人 QQ 资料（仅修改传入的非空字段）。
func (s *Sender) SetQQProfile(ctx stdctx.Context, params SetQQProfileParams) error {
	_, err := s.api.Call(ctx, "set_qq_profile", params)
	return err
}

// SetFriendRemark 修改好友备注名。
func (s *Sender) SetFriendRemark(ctx stdctx.Context, userID int64, remark string) error {
	_, err := s.api.Call(ctx, "set_friend_remark", SetFriendRemarkParams{
		UserID: userID,
		Remark: remark,
	})
	return err
}

// SetInputStatus 设置输入状态（0=输入中，1=离开）。
func (s *Sender) SetInputStatus(ctx stdctx.Context, eventType, times int) error {
	_, err := s.api.Call(ctx, "set_input_status", SetInputStatusParams{
		EventType: eventType,
		Times:     times,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 扩展动作：群管理（LLOneBot/LuckyLilliaBot 扩展）
// ────────────────────────────────────────────────────────────────────────────

// GetGroupShutList 获取群内被禁言成员列表。
func (s *Sender) GetGroupShutList(ctx stdctx.Context, groupID int64) (*GetGroupShutListResult, error) {
	var result GetGroupShutListResult
	if err := callDecoded(ctx, s.api, "get_group_shut_list", GetGroupInfoParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetGroupMsgMask 设置群消息接收屏蔽（mask：1=不提醒，2=不提醒且不入列表，3=完全不接收）。
func (s *Sender) SetGroupMsgMask(ctx stdctx.Context, groupID int64, mask int) error {
	_, err := s.api.Call(ctx, "set_group_msg_mask", SetGroupMsgMaskParams{
		GroupID: groupID,
		Mask:    mask,
	})
	return err
}

// BatchDeleteGroupMember 批量踢出群成员。
//
// 自动兼容协议端动作名与参数差异：LLB 用 batch_delete_group_member
// （字段 user_ids），NapCat 用 set_group_kick_members（字段 user_id）。
func (s *Sender) BatchDeleteGroupMember(ctx stdctx.Context, groupID int64, userIDs []int64, rejectAddRequest bool) error {
	_, err := s.api.Call(ctx, "batch_delete_group_member", BatchDeleteGroupMemberParams{
		GroupID:          groupID,
		UserIDs:          userIDs,
		RejectAddRequest: rejectAddRequest,
	})
	if err == nil || !isActionNotFound(err) {
		return err
	}
	_, err = s.api.Call(ctx, "set_group_kick_members", SetGroupKickMembersParams{
		GroupID:          groupID,
		UserIDs:          userIDs,
		RejectAddRequest: rejectAddRequest,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 扩展动作：AI 角色（LLOneBot/LuckyLilliaBot 扩展）
// ────────────────────────────────────────────────────────────────────────────

// GetAICharacters 获取群内可用的 AI 角色列表。
func (s *Sender) GetAICharacters(ctx stdctx.Context, groupID int64, chatType int) ([]AICharacter, error) {
	var result []AICharacter
	if err := callDecoded(ctx, s.api, "get_ai_characters", GetAICharactersParams{
		GroupID:  groupID,
		ChatType: chatType,
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SendGroupAIRecord 让群内 AI 角色发言。
func (s *Sender) SendGroupAIRecord(ctx stdctx.Context, groupID, characterID int64) (*SendMsgResult, error) {
	var result SendMsgResult
	if err := callDecoded(ctx, s.api, "send_group_ai_record", SendGroupAIRecordParams{
		GroupID:     groupID,
		CharacterID: characterID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetAIRecord 获取 AI 角色的消息记录。
func (s *Sender) GetAIRecord(ctx stdctx.Context, characterID int64) ([]AIRecord, error) {
	var result []AIRecord
	if err := callDecoded(ctx, s.api, "get_ai_record", GetAIRecordParams{CharacterID: characterID}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// 扩展动作：群文件（LLOneBot/LuckyLilliaBot、Lagrange.OneBot v1 扩展）
// ────────────────────────────────────────────────────────────────────────────

// GetGroupFileSystemInfo 获取群文件系统信息（容量与文件数）。
func (s *Sender) GetGroupFileSystemInfo(ctx stdctx.Context, groupID int64) (*GetGroupFileSystemInfoResult, error) {
	var result GetGroupFileSystemInfoResult
	if err := callDecoded(ctx, s.api, "get_group_file_system_info", GroupIDParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGroupRootFiles 获取群文件根目录的文件与文件夹列表。
func (s *Sender) GetGroupRootFiles(ctx stdctx.Context, groupID int64) (*GroupFileListResult, error) {
	var result GroupFileListResult
	if err := callDecoded(ctx, s.api, "get_group_root_files", GroupIDParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGroupFilesByFolder 获取群文件指定文件夹下的文件与文件夹列表。
func (s *Sender) GetGroupFilesByFolder(ctx stdctx.Context, groupID int64, folderID string) (*GroupFileListResult, error) {
	var result GroupFileListResult
	if err := callDecoded(ctx, s.api, "get_group_files_by_folder", GetGroupFilesByFolderParams{
		GroupID:  groupID,
		FolderID: folderID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGroupFileURL 获取群文件下载链接。
func (s *Sender) GetGroupFileURL(ctx stdctx.Context, groupID int64, fileID string) (*URLResult, error) {
	var result URLResult
	if err := callDecoded(ctx, s.api, "get_group_file_url", GetGroupFileURLParams{
		GroupID: groupID,
		FileID:  fileID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetPrivateFileURL 获取私聊文件下载链接。
func (s *Sender) GetPrivateFileURL(ctx stdctx.Context, userID int64, fileID string) (*URLResult, error) {
	var result URLResult
	if err := callDecoded(ctx, s.api, "get_private_file_url", GetPrivateFileURLParams{
		UserID: userID,
		FileID: fileID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// MoveGroupFile 移动群文件到其他文件夹。
func (s *Sender) MoveGroupFile(ctx stdctx.Context, groupID int64, fileID, parentID, targetID string) error {
	_, err := s.api.Call(ctx, "move_group_file", MoveGroupFileParams{
		GroupID:  groupID,
		FileID:   fileID,
		ParentID: parentID,
		TargetID: targetID,
	})
	return err
}

// RenameGroupFile 重命名群文件。
func (s *Sender) RenameGroupFile(ctx stdctx.Context, groupID int64, fileID, newName string) error {
	_, err := s.api.Call(ctx, "rename_group_file", RenameGroupFileParams{
		GroupID: groupID,
		FileID:  fileID,
		NewName: newName,
	})
	return err
}

// DeleteGroupFile 删除群文件。
func (s *Sender) DeleteGroupFile(ctx stdctx.Context, groupID int64, fileID string) error {
	_, err := s.api.Call(ctx, "delete_group_file", DeleteGroupFileParams{
		GroupID: groupID,
		FileID:  fileID,
	})
	return err
}

// CreateGroupFileFolder 在群文件根目录创建文件夹。
func (s *Sender) CreateGroupFileFolder(ctx stdctx.Context, groupID int64, name string) (*CreateGroupFileFolderResult, error) {
	var result CreateGroupFileFolderResult
	if err := callDecoded(ctx, s.api, "create_group_file_folder", CreateGroupFileFolderParams{
		GroupID: groupID,
		Name:    name,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteGroupFolder 删除群文件夹。
// 自动兼容协议端动作名差异：NapCat/LLB 用 delete_group_folder，
// Lagrange.OneBot v1 用 delete_group_file_folder。
func (s *Sender) DeleteGroupFolder(ctx stdctx.Context, groupID int64, folderID string) error {
	_, err := callCompat(ctx, s.api, "delete_group_folder", []string{"delete_group_file_folder"}, DeleteGroupFolderParams{
		GroupID:  groupID,
		FolderID: folderID,
	})
	return err
}

// RenameGroupFileFolder 重命名群文件夹。
func (s *Sender) RenameGroupFileFolder(ctx stdctx.Context, groupID int64, folderID, newName string) error {
	_, err := s.api.Call(ctx, "rename_group_file_folder", RenameGroupFileFolderParams{
		GroupID:  groupID,
		FolderID: folderID,
		NewName:  newName,
	})
	return err
}

// SetGroupFileForever 将群文件转为永久文件。
func (s *Sender) SetGroupFileForever(ctx stdctx.Context, groupID int64, fileID string) error {
	_, err := s.api.Call(ctx, "set_group_file_forever", SetGroupFileForeverParams{
		GroupID: groupID,
		FileID:  fileID,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// LLOneBot/LuckyLilliaBot 专有扩展（2026-08 对照 LLB src/onebot11/action/
// llbot/ 目录源码补齐；Lagrange/NapCat 上调用会返回动作不存在）
// ────────────────────────────────────────────────────────────────────────────

// GetConfig 获取 LLB 当前配置。
func (s *Sender) GetConfig(ctx stdctx.Context) (*LLOneBotConfig, error) {
	var result LLOneBotConfig
	if err := callDecoded(ctx, s.api, "get_config", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetConfig 修改 LLB 配置（仅修改传入的字段）。
func (s *Sender) SetConfig(ctx stdctx.Context, cfg LLOneBotConfig) error {
	_, err := s.api.Call(ctx, "set_config", cfg)
	return err
}

// LLOneBotDebug 调用 LLB 内部 API（调试用，生产环境慎用）。
func (s *Sender) LLOneBotDebug(ctx stdctx.Context, apiClass, method string, args ...any) error {
	_, err := s.api.Call(ctx, "llonebot_debug", LLOneBotDebugParams{
		APIClass: apiClass,
		Method:   method,
		Args:     args,
	})
	return err
}

// GetEvent 获取 HTTP 事件池中的事件（配合 LLB 的 HTTP 事件模式）。
func (s *Sender) GetEvent(ctx stdctx.Context, key string, timeout int) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := callDecoded(ctx, s.api, "get_event", GetEventParams{
		Key:     key,
		Timeout: timeout,
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetRobotUinRange 获取 LLB 支持的机器人 QQ 号段。
func (s *Sender) GetRobotUinRange(ctx stdctx.Context) ([]GetRobotUinRangeResult, error) {
	var result []GetRobotUinRangeResult
	if err := callDecoded(ctx, s.api, "get_robot_uin_range", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SendPB 发送底层 PB 协议包（高危操作，需了解 QQ 协议）。
func (s *Sender) SendPB(ctx stdctx.Context, cmd, hex string) (*SendPBResult, error) {
	var result SendPBResult
	if err := callDecoded(ctx, s.api, "send_pb", SendPBParams{Cmd: cmd, Hex: hex}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ScanQRCode 扫描本地二维码图片（LLB 当前实现返回"暂不支持"）。
func (s *Sender) ScanQRCode(ctx stdctx.Context, file string) ([]ScanQRCodeResult, error) {
	var result []ScanQRCodeResult
	if err := callDecoded(ctx, s.api, "scan_qrcode", FilePathParams{File: file}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetRkey 获取 QQ 文件下载 RKey（时效性凭证）。
func (s *Sender) GetRkey(ctx stdctx.Context) (*GetRkeyResult, error) {
	var result GetRkeyResult
	if err := callDecoded(ctx, s.api, "get_rkey", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ForwardFriendSingleMsg 向好友转发单条消息。
func (s *Sender) ForwardFriendSingleMsg(ctx stdctx.Context, messageID, userID int64) (*SendMsgResult, error) {
	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "forward_friend_single_msg", ForwardSingleMsgParams{
		MessageID: messageID,
		UserID:    userID,
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ForwardGroupSingleMsg 向群转发单条消息。
func (s *Sender) ForwardGroupSingleMsg(ctx stdctx.Context, messageID, groupID int64) (*SendMsgResult, error) {
	var res SendMsgResult
	if err := callDecoded(ctx, s.api, "forward_group_single_msg", ForwardSingleMsgParams{
		MessageID: messageID,
		GroupID:   groupID,
	}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// FetchCustomFace 获取账号的自定义表情 URL 列表。
func (s *Sender) FetchCustomFace(ctx stdctx.Context) ([]string, error) {
	var result []string
	if err := callDecoded(ctx, s.api, "fetch_custom_face", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// VoiceMsg2Text 将语音消息转写为文字。
// 自动兼容协议端动作名差异：LLB 用 voice_msg_to_text，NapCat 用 fetch_ptt_text。
func (s *Sender) VoiceMsg2Text(ctx stdctx.Context, messageID int64) (*VoiceMsg2TextResult, error) {
	var result VoiceMsg2TextResult
	if err := callCompatDecoded(ctx, s.api, "voice_msg_to_text", []string{"fetch_ptt_text"}, MessageIDParams{MessageID: messageID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetRecommendFace 按关键词搜索推荐表情，返回表情 URL 列表。
func (s *Sender) GetRecommendFace(ctx stdctx.Context, word string) (*GetRecommendFaceResult, error) {
	var result GetRecommendFaceResult
	if err := callDecoded(ctx, s.api, "get_recommend_face", GetRecommendFaceParams{Word: word}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetOnlineStatus 设置机器人在线状态。
func (s *Sender) SetOnlineStatus(ctx stdctx.Context, status, extStatus, batteryStatus int) error {
	_, err := s.api.Call(ctx, "set_online_status", SetOnlineStatusParams{
		Status:        status,
		ExtStatus:     extStatus,
		BatteryStatus: batteryStatus,
	})
	return err
}

// GetProfileLike 获取收到我的名片赞的用户列表。
func (s *Sender) GetProfileLike(ctx stdctx.Context, start, count int) (*GetProfileLikeResult, error) {
	var result GetProfileLikeResult
	if err := callDecoded(ctx, s.api, "get_profile_like", GetProfileLikeParams{
		Start: start,
		Count: count,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProfileLikeMe 获取我发出的名片赞的用户列表。
func (s *Sender) GetProfileLikeMe(ctx stdctx.Context, start, count int) (*GetProfileLikeResult, error) {
	var result GetProfileLikeResult
	if err := callDecoded(ctx, s.api, "get_profile_like_me", GetProfileLikeParams{
		Start: start,
		Count: count,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetProfileLikeCount 获取用户收到的名片赞总数。
func (s *Sender) GetProfileLikeCount(ctx stdctx.Context, userID int64) (*GetProfileLikeCountResult, error) {
	var result GetProfileLikeCountResult
	if err := callDecoded(ctx, s.api, "get_profile_like_count", UserIDParams{UserID: userID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFriendsWithCategory 获取按分组组织的完整好友列表。
func (s *Sender) GetFriendsWithCategory(ctx stdctx.Context) ([]FriendCategoryWithList, error) {
	var result []FriendCategoryWithList
	if err := callDecoded(ctx, s.api, "get_friends_with_category", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetFriendCategory 将好友移动到指定分组。
func (s *Sender) SetFriendCategory(ctx stdctx.Context, userID, categoryID int64) error {
	_, err := s.api.Call(ctx, "set_friend_category", SetFriendCategoryParams{
		UserID:     userID,
		CategoryID: categoryID,
	})
	return err
}

// GetDoubtFriendsAddRequest 获取风险（疑似诈骗）好友请求列表。
func (s *Sender) GetDoubtFriendsAddRequest(ctx stdctx.Context, count int) ([]DoubtFriendRequest, error) {
	var result []DoubtFriendRequest
	if err := callDecoded(ctx, s.api, "get_doubt_friends_add_request", CountParams{Count: count}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetDoubtFriendsAddRequest 通过风险好友请求（flag 来自 get_doubt_friends_add_request）。
func (s *Sender) SetDoubtFriendsAddRequest(ctx stdctx.Context, flag string) error {
	_, err := s.api.Call(ctx, "set_doubt_friends_add_request", SetDoubtFriendsAddRequestParams{Flag: flag})
	return err
}

// SetGroupRemark 设置群备注。
func (s *Sender) SetGroupRemark(ctx stdctx.Context, groupID int64, remark string) error {
	_, err := s.api.Call(ctx, "set_group_remark", SetGroupRemarkParams{
		GroupID: groupID,
		Remark:  remark,
	})
	return err
}

// GetGroupSignedList 获取群签到列表。
func (s *Sender) GetGroupSignedList(ctx stdctx.Context, groupID int64) ([]GroupSignedMember, error) {
	var result []GroupSignedMember
	if err := callDecoded(ctx, s.api, "get_group_signed_list", GetGroupInfoParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteGroupNotice 删除群公告（LLB 隐藏动作 _delete_group_notice）。
func (s *Sender) DeleteGroupNotice(ctx stdctx.Context, groupID int64, noticeID string) error {
	_, err := s.api.Call(ctx, "_delete_group_notice", DeleteGroupNoticeParams{
		GroupID:  groupID,
		NoticeID: noticeID,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// LLOneBot/LuckyLilliaBot 专有扩展：闪传文件
// ────────────────────────────────────────────────────────────────────────────

// UploadFlashFile 上传闪传文件集（paths 支持本地路径或 URL）。
func (s *Sender) UploadFlashFile(ctx stdctx.Context, title string, paths []string) (*FlashFileSetResult, error) {
	var result FlashFileSetResult
	if err := callDecoded(ctx, s.api, "upload_flash_file", UploadFlashFileParams{
		Title: title,
		Paths: paths,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadFlashFile 下载闪传文件（share_link 与 fileSetID 二选一）。
func (s *Sender) DownloadFlashFile(ctx stdctx.Context, params FlashFileParams) error {
	_, err := s.api.Call(ctx, "download_flash_file", params)
	return err
}

// GetFlashFileInfo 获取闪传文件信息（标题、文件列表等）。
func (s *Sender) GetFlashFileInfo(ctx stdctx.Context, params FlashFileParams) (*GetFlashFileInfoResult, error) {
	var result GetFlashFileInfoResult
	if err := callDecoded(ctx, s.api, "get_flash_file_info", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFlashFileDownloadUrls 获取闪传文件集内所有文件的下载入口。
func (s *Sender) GetFlashFileDownloadUrls(ctx stdctx.Context, params FlashFileParams) (*GetFlashFileDownloadUrlsResult, error) {
	var result GetFlashFileDownloadUrlsResult
	if err := callDecoded(ctx, s.api, "get_flash_file_download_urls", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ReShareFlashFile 重新分享闪传文件（生成全新 14 天有效期的分享链接）。
func (s *Sender) ReShareFlashFile(ctx stdctx.Context, params FlashFileParams) (*FlashFileSetResult, error) {
	var result FlashFileSetResult
	if err := callDecoded(ctx, s.api, "reshare_flash_file", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// LLOneBot/LuckyLilliaBot 专有扩展：群相册
// ────────────────────────────────────────────────────────────────────────────

// GetGroupAlbumList 获取群相册列表。
// 自动兼容协议端动作名差异：LLB 用 get_group_album_list，
// NapCat 用 get_qun_album_list（attachInfo 为 NapCat 分页游标）。
func (s *Sender) GetGroupAlbumList(ctx stdctx.Context, groupID int64, attachInfo ...string) ([]GroupAlbum, error) {
	params := GetQunAlbumListParams{GroupID: groupID}
	if len(attachInfo) > 0 {
		params.AttachInfo = attachInfo[0]
	}
	var result []GroupAlbum
	if err := callCompatDecoded(ctx, s.api, "get_group_album_list", []string{"get_qun_album_list"}, params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CreateGroupAlbum 创建群相册。
func (s *Sender) CreateGroupAlbum(ctx stdctx.Context, groupID int64, name, desc string) (*CreateGroupAlbumResult, error) {
	var result CreateGroupAlbumResult
	if err := callDecoded(ctx, s.api, "create_group_album", CreateGroupAlbumParams{
		GroupID: groupID,
		Name:    name,
		Desc:    desc,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteGroupAlbum 删除群相册。
func (s *Sender) DeleteGroupAlbum(ctx stdctx.Context, groupID int64, albumID string) error {
	_, err := s.api.Call(ctx, "delete_group_album", DeleteGroupAlbumParams{
		GroupID: groupID,
		AlbumID: albumID,
	})
	return err
}

// UploadGroupAlbum 向群相册上传图片（files 支持本地路径或 URL）。
func (s *Sender) UploadGroupAlbum(ctx stdctx.Context, groupID int64, albumID string, files []string) error {
	_, err := s.api.Call(ctx, "upload_group_album", UploadGroupAlbumParams{
		GroupID: groupID,
		AlbumID: albumID,
		Files:   files,
	})
	return err
}

// GetGroupAlbumMediaList 分页获取群相册媒体列表（attachInfo 为分页游标）。
func (s *Sender) GetGroupAlbumMediaList(ctx stdctx.Context, groupID int64, albumID, attachInfo string) (*GetGroupAlbumMediaListResult, error) {
	var result GetGroupAlbumMediaListResult
	if err := callDecoded(ctx, s.api, "get_group_album_media_list", GetGroupAlbumMediaListParams{
		GroupID:    groupID,
		AlbumID:    albumID,
		AttachInfo: attachInfo,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// NapCat 补充动作（2026-08 对照 NapCat packages/napcat-onebot/action/ 源码
// 补齐；部分动作 LLB/Lagrange 不存在，调用会返回动作不存在）
// ────────────────────────────────────────────────────────────────────────────

// GetEmojiLikes 获取消息表情回应的用户列表（NapCat 动作名，等价于 LLB 的
// fetch_emoji_like）。
func (s *Sender) GetEmojiLikes(ctx stdctx.Context, params GetEmojiLikesParams) ([]GetEmojiLikesResult, error) {
	var result []GetEmojiLikesResult
	if err := callDecoded(ctx, s.api, "get_emoji_likes", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetUnidirectionalFriendList 获取单向好友列表（NapCat 扩展）。
func (s *Sender) GetUnidirectionalFriendList(ctx stdctx.Context) ([]FriendInfo, error) {
	var result []FriendInfo
	if err := callDecoded(ctx, s.api, "get_unidirectional_friend_list", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteUnidirectionalFriend 删除单向好友（NapCat 扩展）。
func (s *Sender) DeleteUnidirectionalFriend(ctx stdctx.Context, userID int64) error {
	_, err := s.api.Call(ctx, "delete_unidirectional_friend", UserIDParams{UserID: userID})
	return err
}

// ClickInlineKeyboardButton 模拟点击内联键盘按钮（NapCat 扩展）。
func (s *Sender) ClickInlineKeyboardButton(ctx stdctx.Context, params ClickInlineKeyboardButtonParams) error {
	_, err := s.api.Call(ctx, "click_inline_keyboard_button", params)
	return err
}

// GetRecentContact 获取最近会话列表（NapCat 扩展）。
func (s *Sender) GetRecentContact(ctx stdctx.Context) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := callDecoded(ctx, s.api, "get_recent_contact", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetOnlineClients 获取账号在线客户端列表（NapCat 扩展）。
func (s *Sender) GetOnlineClients(ctx stdctx.Context) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := callDecoded(ctx, s.api, "get_online_clients", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetGroupMemberInvitePolicy 设置群入群审核策略（NapCat 扩展）。
func (s *Sender) SetGroupMemberInvitePolicy(ctx stdctx.Context, groupID int64, policy int) error {
	_, err := s.api.Call(ctx, "set_group_member_invite_policy", SetGroupMemberInvitePolicyParams{
		GroupID: groupID,
		Policy:  policy,
	})
	return err
}

// SetGroupMemberPermissions 设置群成员权限（NapCat 扩展，nil 字段不修改）。
func (s *Sender) SetGroupMemberPermissions(ctx stdctx.Context, params SetGroupMemberPermissionsParams) error {
	_, err := s.api.Call(ctx, "set_group_member_permissions", params)
	return err
}

// SetGroupNewMemberHistoryVisibility 设置新成员可见聊天记录（NapCat 扩展）。
func (s *Sender) SetGroupNewMemberHistoryVisibility(ctx stdctx.Context, groupID int64, visible bool) error {
	_, err := s.api.Call(ctx, "set_group_new_member_history_visibility", SetGroupNewMemberHistoryVisibilityParams{
		GroupID: groupID,
		Visible: visible,
	})
	return err
}

// GetMiniAppArk 获取小程序 ARK 模板（NapCat 扩展）。
func (s *Sender) GetMiniAppArk(ctx stdctx.Context) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := callDecoded(ctx, s.api, "get_mini_app_ark", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SendArkShare 发送 ARK 分享消息（NapCat 标准接口）。
// params 为 ARK 模板数据，结构随协议端文档变化，调用方按需构造。
func (s *Sender) SendArkShare(ctx stdctx.Context, params map[string]any) error {
	_, err := s.api.Call(ctx, "send_ark_share", params)
	return err
}

// SendGroupArkShare 向群发送 ARK 分享消息（NapCat 标准接口）。
func (s *Sender) SendGroupArkShare(ctx stdctx.Context, params map[string]any) error {
	_, err := s.api.Call(ctx, "send_group_ark_share", params)
	return err
}

// SetDiyOnlineStatus 设置自定义在线状态（NapCat 扩展）。
func (s *Sender) SetDiyOnlineStatus(ctx stdctx.Context, faceID, faceType int, wording string) error {
	_, err := s.api.Call(ctx, "set_diy_online_status", SetDiyOnlineStatusParams{
		FaceID:   faceID,
		FaceType: faceType,
		Wording:  wording,
	})
	return err
}

// SetGroupSearch 设置群搜索相关选项（NapCat 扩展）。
func (s *Sender) SetGroupSearch(ctx stdctx.Context, groupID int64, noCodeFingerOpen, noFingerOpen int) error {
	_, err := s.api.Call(ctx, "set_group_search", SetGroupSearchParams{
		GroupID:          groupID,
		NoCodeFingerOpen: noCodeFingerOpen,
		NoFingerOpen:     noFingerOpen,
	})
	return err
}

// TranslateEn2Zh 英文单词批量翻译为中文（NapCat 扩展）。
func (s *Sender) TranslateEn2Zh(ctx stdctx.Context, words []string) ([]TranslateEn2ZhResult, error) {
	var result []TranslateEn2ZhResult
	if err := callDecoded(ctx, s.api, "translate_en2zh", TranslateEn2ZhParams{Words: words}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CheckUrlSafely 检查 URL 安全性（NapCat 扩展，level：1=安全 2=未知 3=危险）。
func (s *Sender) CheckUrlSafely(ctx stdctx.Context, url string) (*CheckUrlSafelyResult, error) {
	var result CheckUrlSafelyResult
	if err := callDecoded(ctx, s.api, "check_url_safely", URLParams{URL: url}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// FetchCustomFaceDetail 获取自定义表情详情列表（NapCat 扩展）。
func (s *Sender) FetchCustomFaceDetail(ctx stdctx.Context, count int) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := callDecoded(ctx, s.api, "fetch_custom_face_detail", CountParams{Count: count}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AddCustomFace 添加自定义表情（NapCat 扩展，file 为本地路径）。
func (s *Sender) AddCustomFace(ctx stdctx.Context, params AddCustomFaceParams) error {
	_, err := s.api.Call(ctx, "add_custom_face", params)
	return err
}

// DeleteCustomFace 删除自定义表情（NapCat 扩展）。
func (s *Sender) DeleteCustomFace(ctx stdctx.Context, params DeleteCustomFaceParams) error {
	_, err := s.api.Call(ctx, "delete_custom_face", params)
	return err
}

// SetCustomFaceDesc 修改自定义表情描述（NapCat 扩展）。
func (s *Sender) SetCustomFaceDesc(ctx stdctx.Context, params SetCustomFaceDescParams) error {
	_, err := s.api.Call(ctx, "set_custom_face_desc", params)
	return err
}

// SetGroupAddOption 设置群加群方式（NapCat 扩展）。
func (s *Sender) SetGroupAddOption(ctx stdctx.Context, params SetGroupAddOptionParams) error {
	_, err := s.api.Call(ctx, "set_group_add_option", params)
	return err
}

// SetGroupRobotAddOption 设置群机器人相关选项（NapCat 扩展）。
func (s *Sender) SetGroupRobotAddOption(ctx stdctx.Context, params SetGroupRobotAddOptionParams) error {
	_, err := s.api.Call(ctx, "set_group_robot_add_option", params)
	return err
}

// GetGroupIgnoredNotifies 获取被过滤的加群通知。
// 自动兼容协议端动作名差异：NapCat 用 get_group_ignored_notifies，
// LLB 用 get_group_ignore_add_request。
func (s *Sender) GetGroupIgnoredNotifies(ctx stdctx.Context) ([]GroupIgnoredNotify, error) {
	var result []GroupIgnoredNotify
	if err := callCompatDecoded(ctx, s.api, "get_group_ignored_notifies", []string{"get_group_ignore_add_request"}, struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupDetailInfo 获取群详情。
// 自动兼容协议端动作名差异：NapCat 用 get_group_detail_info，
// 部分实现用 get_group_info_ex。
func (s *Sender) GetGroupDetailInfo(ctx stdctx.Context, groupID int64) (*GroupInfoEx, error) {
	var result GroupInfoEx
	if err := callCompatDecoded(ctx, s.api, "get_group_detail_info", []string{"get_group_info_ex"}, GetGroupInfoParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ────────────────────────────────────────────────────────────────────────────
// NapCat 闪传文件（file/flash/ 目录，2026-08 对照源码补齐）
// ────────────────────────────────────────────────────────────────────────────

// CreateFlashTask 创建闪传任务（NapCat 扩展）。
func (s *Sender) CreateFlashTask(ctx stdctx.Context, files []string, name, thumbPath string) (*FlashFileSetResult, error) {
	var result FlashFileSetResult
	if err := callDecoded(ctx, s.api, "create_flash_task", CreateFlashTaskParams{
		Files:     files,
		Name:      name,
		ThumbPath: thumbPath,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DownloadFileset 下载闪传文件集（NapCat 扩展）。
func (s *Sender) DownloadFileset(ctx stdctx.Context, fileSetID string) error {
	_, err := s.api.Call(ctx, "download_fileset", GetFlashFileListParams{FileSetID: fileSetID})
	return err
}

// GetFilesetId 由分享链接获取闪传文件集 ID（NapCat 扩展）。
func (s *Sender) GetFilesetId(ctx stdctx.Context, shareLink string) (*GetFilesetIdResult, error) {
	var result GetFilesetIdResult
	if err := callDecoded(ctx, s.api, "get_fileset_id", GetFilesetIdParams{ShareLink: shareLink}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFilesetInfo 获取闪传文件集信息（NapCat 扩展）。
func (s *Sender) GetFilesetInfo(ctx stdctx.Context, fileSetID string) (*FilesetInfo, error) {
	var result FilesetInfo
	if err := callDecoded(ctx, s.api, "get_fileset_info", GetFlashFileListParams{FileSetID: fileSetID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetFlashFileList 获取闪传文件集内的文件列表（NapCat 扩展）。
func (s *Sender) GetFlashFileList(ctx stdctx.Context, fileSetID string) ([]FlashFileEntry, error) {
	var result []FlashFileEntry
	if err := callDecoded(ctx, s.api, "get_flash_file_list", GetFlashFileListParams{FileSetID: fileSetID}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetFlashFileUrl 获取闪传文件下载链接（NapCat 扩展）。
func (s *Sender) GetFlashFileUrl(ctx stdctx.Context, fileSetID, fileName string, fileIndex int) (*GetFlashFileUrlResult, error) {
	var result GetFlashFileUrlResult
	if err := callDecoded(ctx, s.api, "get_flash_file_url", GetFlashFileUrlParams{
		FileSetID: fileSetID,
		FileName:  fileName,
		FileIndex: fileIndex,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetShareLink 获取闪传文件集分享链接（NapCat 扩展）。
func (s *Sender) GetShareLink(ctx stdctx.Context, fileSetID string) (*GetShareLinkResult, error) {
	var result GetShareLinkResult
	if err := callDecoded(ctx, s.api, "get_share_link", GetShareLinkParams{FileSetID: fileSetID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendFlashMsg 发送闪传消息（NapCat 扩展，user_id 与 group_id 至少填一个）。
func (s *Sender) SendFlashMsg(ctx stdctx.Context, fileSetID string, userID, groupID int64) error {
	_, err := s.api.Call(ctx, "send_flash_msg", SendFlashMsgParams{
		FileSetID: fileSetID,
		UserID:    userID,
		GroupID:   groupID,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// Lagrange.OneBot v1 补充动作（2026-08 对照 Lagrange.Core v1 分支源码补齐；
// 多数协议端已弃用 OneBot，动作在 NapCat/LLB 上可能不存在）
// ────────────────────────────────────────────────────────────────────────────

// GetMusicArk 获取音乐 ARK 模板（go-cqhttp/Lagrange 扩展）。
// params 结构随协议端文档变化，调用方按需构造（如 {"id": "...", "music_type": 0}）。
func (s *Sender) GetMusicArk(ctx stdctx.Context, params map[string]any) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := callDecoded(ctx, s.api, "get_music_ark", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// FetchMFaceKey 获取商城表情 Key（Lagrange v1 扩展）。
func (s *Sender) FetchMFaceKey(ctx stdctx.Context, emojiIDs []string) ([]FetchMFaceKeyResult, error) {
	var result []FetchMFaceKeyResult
	if err := callDecoded(ctx, s.api, "fetch_mface_key", FetchMFaceKeyParams{EmojiIDs: emojiIDs}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupMemo 获取群备忘录（Lagrange v1 扩展）。
func (s *Sender) GetGroupMemo(ctx stdctx.Context, groupID int64) ([]GroupMemo, error) {
	var result []GroupMemo
	if err := callDecoded(ctx, s.api, "get_group_memo", GetGroupInfoParams{GroupID: groupID}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetGroupMemo 设置群备忘录（Lagrange v1 扩展）。
func (s *Sender) SetGroupMemo(ctx stdctx.Context, groupID int64, content string) error {
	_, err := s.api.Call(ctx, "set_group_memo", SetGroupMemoParams{
		GroupID: groupID,
		Content: content,
	})
	return err
}

// DeleteGroupMemo 删除群备忘录（Lagrange v1 扩展）。
func (s *Sender) DeleteGroupMemo(ctx stdctx.Context, groupID int64, memoID string) error {
	_, err := s.api.Call(ctx, "delete_group_memo", DeleteGroupMemoParams{
		GroupID: groupID,
		MemoID:  memoID,
	})
	return err
}

// GetGroupRequests 获取群请求列表（Lagrange v1 扩展）。
func (s *Sender) GetGroupRequests(ctx stdctx.Context, params map[string]any) ([]json.RawMessage, error) {
	var result []json.RawMessage
	if err := callDecoded(ctx, s.api, "get_group_requests", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SetGroupBotStatus 设置群内机器人状态（Lagrange v1 扩展）。
func (s *Sender) SetGroupBotStatus(ctx stdctx.Context, groupID int64, online bool) error {
	_, err := s.api.Call(ctx, "set_group_bot_status", SetGroupBotStatusParams{
		GroupID: groupID,
		Online:  online,
	})
	return err
}

// UploadImage 上传图片并返回 URL（Lagrange v1 扩展）。
func (s *Sender) UploadImage(ctx stdctx.Context, file string) (*URLResult, error) {
	var result URLResult
	if err := callDecoded(ctx, s.api, "upload_image", FilePathParams{File: file}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendPacket 发送底层数据包（Lagrange v1 隐藏动作 .send_packet，高危）。
func (s *Sender) SendPacket(ctx stdctx.Context, params map[string]any) error {
	_, err := s.api.Call(ctx, ".send_packet", params)
	return err
}

// FriendJoinEmojiChain 加入好友表情接力（Lagrange v1 隐藏动作）。
func (s *Sender) FriendJoinEmojiChain(ctx stdctx.Context, params map[string]any) error {
	_, err := s.api.Call(ctx, ".join_friend_emoji_chain", params)
	return err
}

// GroupJoinEmojiChain 加入群表情接力（Lagrange v1 隐藏动作）。
func (s *Sender) GroupJoinEmojiChain(ctx stdctx.Context, params map[string]any) error {
	_, err := s.api.Call(ctx, ".join_group_emoji_chain", params)
	return err
}
