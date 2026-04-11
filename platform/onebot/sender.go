package onebot

import (
	stdctx "context"
	"fmt"
	"strconv"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// ────────────────────────────────────────────────────────────────────────────
// onebotSender
// ────────────────────────────────────────────────────────────────────────────

// onebotSender 实现了以下 platform 接口：
//   - platform.Sender
//   - platform.MessageDeleter  (delete_msg)
//   - platform.GroupManager    (kick / ban / set_admin)
//   - platform.InvitationHandler (好友/群请求)
//   - platform.AutoModerator   (全体禁言)
type onebotSender struct {
	api APIClient
}

// newSender 创建一个由指定 APIClient 驱动的 onebotSender。
func newSender(api APIClient) *onebotSender {
	return &onebotSender{api: api}
}

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
func (s *onebotSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
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

func (s *onebotSender) sendGroup(ctx stdctx.Context, target platform.ChatInfo, chain MessageChain) (platform.SendResult, error) {
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
		return platform.SendResult{}, err
	}

	return platform.SendResult{
		Platform:  PlatformID,
		MessageID: strconv.FormatInt(int64(res.MessageID), 10),
		Timestamp: time.Now(),
	}, nil
}

func (s *onebotSender) sendPrivate(ctx stdctx.Context, target platform.ChatInfo, chain MessageChain) (platform.SendResult, error) {
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
		return platform.SendResult{}, err
	}

	return platform.SendResult{
		Platform:  PlatformID,
		MessageID: strconv.FormatInt(int64(res.MessageID), 10),
		Timestamp: time.Now(),
	}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// platform.MessageDeleter
// ────────────────────────────────────────────────────────────────────────────

// Delete 通过调用 delete_msg 实现 platform.MessageDeleter。
//
// chatID 参数被忽略（OneBot delete_msg 只需消息 ID）。
func (s *onebotSender) Delete(ctx stdctx.Context, _ string, messageID string) error {
	msgID, err := strconv.ParseInt(messageID, 10, 32)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid message_id %q: %w", messageID, err)
	}
	_, err = s.api.Call(ctx, "delete_msg", DeleteMsgParams{MessageID: int32(msgID)})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// platform.GroupManager
// ────────────────────────────────────────────────────────────────────────────

// KickMember 通过调用 set_group_kick 实现 platform.GroupManager。
func (s *onebotSender) KickMember(ctx stdctx.Context, groupID, userID string, permanent bool) error {
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
func (s *onebotSender) BanMember(ctx stdctx.Context, groupID, userID string, duration time.Duration) error {
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
func (s *onebotSender) SetAdmin(ctx stdctx.Context, groupID, userID string, isAdmin bool) error {
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
func (s *onebotSender) AcceptGroupInvite(ctx stdctx.Context, inviteID string) error {
	_, err := s.api.Call(ctx, "set_group_add_request", SetGroupAddRequestParams{
		Flag:    inviteID,
		SubType: GroupRequestInvite,
		Approve: true,
	})
	return err
}

// RejectGroupInvite 实现 platform.InvitationHandler。
func (s *onebotSender) RejectGroupInvite(ctx stdctx.Context, inviteID, reason string) error {
	_, err := s.api.Call(ctx, "set_group_add_request", SetGroupAddRequestParams{
		Flag:    inviteID,
		SubType: GroupRequestInvite,
		Approve: false,
		Reason:  reason,
	})
	return err
}

// AcceptFriendRequest 实现 platform.InvitationHandler。
func (s *onebotSender) AcceptFriendRequest(ctx stdctx.Context, requestID string) error {
	_, err := s.api.Call(ctx, "set_friend_add_request", SetFriendAddRequestParams{
		Flag:    requestID,
		Approve: true,
	})
	return err
}

// RejectFriendRequest 实现 platform.InvitationHandler。
func (s *onebotSender) RejectFriendRequest(ctx stdctx.Context, requestID, _ string) error {
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
func (s *onebotSender) DeleteMemberMessage(ctx stdctx.Context, _ string, messageID string) error {
	return s.Delete(ctx, "", messageID)
}

// MuteAll 通过调用 set_group_whole_ban 实现 platform.AutoModerator。
func (s *onebotSender) MuteAll(ctx stdctx.Context, groupID string, mute bool) error {
	gid, err := strconv.ParseInt(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("onebot sender: invalid group_id: %w", err)
	}
	_, err = s.api.Call(ctx, "set_group_whole_ban", SetGroupWholeBanParams{
		GroupID: gid,
		Enable:  mute,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 消息相关扩展接口
// ────────────────────────────────────────────────────────────────────────────

// GetMsg 获取指定消息的详细信息。
func (s *onebotSender) GetMsg(ctx stdctx.Context, messageID int32) (*GetMsgResult, error) {
	var result GetMsgResult
	if err := callDecoded(ctx, s.api, "get_msg", GetMsgParams{MessageID: messageID}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetForwardMsg 获取合并转发消息的内容。
func (s *onebotSender) GetForwardMsg(ctx stdctx.Context, id string) (*GetForwardMsgResult, error) {
	var result GetForwardMsgResult
	if err := callDecoded(ctx, s.api, "get_forward_msg", GetForwardMsgParams{ID: id}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendLike 给指定用户发送好友赞。
// times 为点赞次数（上限 10），传 0 时使用默认值 1。
func (s *onebotSender) SendLike(ctx stdctx.Context, userID int64, times int) error {
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
func (s *onebotSender) SetGroupCard(ctx stdctx.Context, groupID, userID, card string) error {
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
func (s *onebotSender) SetGroupName(ctx stdctx.Context, groupID, name string) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_name", SetGroupNameParams{
		GroupID:   gid,
		GroupName: name,
	})
	return err
}

// LeaveGroup 退出（或解散）群组。
func (s *onebotSender) LeaveGroup(ctx stdctx.Context, groupID string, dismiss bool) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_leave", SetGroupLeaveParams{
		GroupID:   gid,
		IsDismiss: dismiss,
	})
	return err
}

// SetGroupSpecialTitle 设置或移除群成员的专属头衔。
func (s *onebotSender) SetGroupSpecialTitle(ctx stdctx.Context, groupID, userID, title string) error {
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
func (s *onebotSender) BanAnonymous(ctx stdctx.Context, groupID string, anon *AnonymousInfo, duration time.Duration) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_anonymous_ban", SetGroupAnonymousBanParams{
		GroupID:   gid,
		Anonymous: anon,
		Duration:  int64(duration.Seconds()),
	})
	return err
}

// SetGroupAnonymous 开启或关闭群匿名发言功能。
func (s *onebotSender) SetGroupAnonymous(ctx stdctx.Context, groupID string, enable bool) error {
	gid, _ := strconv.ParseInt(groupID, 10, 64)
	_, err := s.api.Call(ctx, "set_group_anonymous", SetGroupAnonymousParams{
		GroupID: gid,
		Enable:  enable,
	})
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// 账号信息查询接口
// ────────────────────────────────────────────────────────────────────────────

// GetLoginInfo 获取机器人自身的 QQ 号和昵称。
func (s *onebotSender) GetLoginInfo(ctx stdctx.Context) (*GetLoginInfoResult, error) {
	var result GetLoginInfoResult
	if err := callDecoded(ctx, s.api, "get_login_info", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetStrangerInfo 获取陌生人信息。
// noCache 为 true 时不使用缓存，直接向服务器请求。
func (s *onebotSender) GetStrangerInfo(ctx stdctx.Context, userID int64, noCache bool) (*StrangerInfo, error) {
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
func (s *onebotSender) GetFriendList(ctx stdctx.Context) ([]FriendInfo, error) {
	var result []FriendInfo
	if err := callDecoded(ctx, s.api, "get_friend_list", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupInfo 获取群信息。
// noCache 为 true 时不使用缓存，直接向服务器请求。
func (s *onebotSender) GetGroupInfo(ctx stdctx.Context, groupID int64, noCache bool) (*GroupInfo, error) {
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
func (s *onebotSender) GetGroupList(ctx stdctx.Context) ([]GroupInfo, error) {
	var result []GroupInfo
	if err := callDecoded(ctx, s.api, "get_group_list", struct{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupMemberInfo 获取群成员信息。
// noCache 为 true 时不使用缓存，直接向服务器请求。
func (s *onebotSender) GetGroupMemberInfo(ctx stdctx.Context, groupID, userID int64, noCache bool) (*GroupMemberInfo, error) {
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
func (s *onebotSender) GetGroupMemberList(ctx stdctx.Context, groupID int64) ([]GroupMemberInfo, error) {
	var result []GroupMemberInfo
	if err := callDecoded(ctx, s.api, "get_group_member_list", GetGroupMemberListParams{
		GroupID: groupID,
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetGroupHonorInfo 获取群荣誉信息。
// honorType 可为 "talkative"、"performer"、"legend"、"strong_newbie"、"emotion" 或 "all"。
func (s *onebotSender) GetGroupHonorInfo(ctx stdctx.Context, groupID int64, honorType string) (*GroupHonorInfo, error) {
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
func (s *onebotSender) GetCookies(ctx stdctx.Context, domain string) (*GetCookiesResult, error) {
	var result GetCookiesResult
	if err := callDecoded(ctx, s.api, "get_cookies", GetCookiesParams{Domain: domain}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetCSRFToken 获取 CSRF Token。
func (s *onebotSender) GetCSRFToken(ctx stdctx.Context) (*GetCSRFTokenResult, error) {
	var result GetCSRFTokenResult
	if err := callDecoded(ctx, s.api, "get_csrf_token", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetCredentials 获取 QQ 相关接口凭证（Cookies + CSRF Token 的合并版本）。
func (s *onebotSender) GetCredentials(ctx stdctx.Context, domain string) (*GetCredentialsResult, error) {
	var result GetCredentialsResult
	if err := callDecoded(ctx, s.api, "get_credentials", GetCredentialsParams{Domain: domain}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetRecord 获取语音文件，转换为指定格式后返回本地路径。
// outFormat 可为 mp3、amr、wma、m4a、spx、ogg、wav、flac。
func (s *onebotSender) GetRecord(ctx stdctx.Context, file, outFormat string) (*GetRecordResult, error) {
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
func (s *onebotSender) GetImage(ctx stdctx.Context, file string) (*GetImageResult, error) {
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
func (s *onebotSender) CanSendImage(ctx stdctx.Context) (bool, error) {
	var result CanSendResult
	if err := callDecoded(ctx, s.api, "can_send_image", struct{}{}, &result); err != nil {
		return false, err
	}
	return result.Yes, nil
}

// CanSendRecord 检查当前账号是否可以发送语音。
func (s *onebotSender) CanSendRecord(ctx stdctx.Context) (bool, error) {
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
func (s *onebotSender) GetStatus(ctx stdctx.Context) (*GetStatusResult, error) {
	var result GetStatusResult
	if err := callDecoded(ctx, s.api, "get_status", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetVersionInfo 获取 OneBot 实现的版本信息。
func (s *onebotSender) GetVersionInfo(ctx stdctx.Context) (*GetVersionInfoResult, error) {
	var result GetVersionInfoResult
	if err := callDecoded(ctx, s.api, "get_version_info", struct{}{}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SetRestart 重启 OneBot 实现。
// delay 为重启前的等待毫秒数，传 0 时立即重启。
func (s *onebotSender) SetRestart(ctx stdctx.Context, delay int) error {
	_, err := s.api.Call(ctx, "set_restart", SetRestartParams{Delay: delay})
	return err
}

// CleanCache 清理 OneBot 实现的本地缓存（如图片、语音等）。
func (s *onebotSender) CleanCache(ctx stdctx.Context) error {
	_, err := s.api.Call(ctx, "clean_cache", struct{}{})
	return err
}
