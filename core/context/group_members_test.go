package context

// group_members_test.go — 群成员查询错误可见性的回归测试
//
// 回归背景：TryGetGroupMembers 把平台返回的错误（网络/权限/限流）与
// "平台不支持该能力"折叠成同一个 false，下游故障表现为"平台不支持"，
// 调用方既无法告警也不会重试。

import (
	stdctx "context"
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// groupInfoSenderStub 实现 platform.GroupInfoProvider，只覆盖成员列表查询行为。
type groupInfoSenderStub struct {
	platform.NoopSender
	listFn func(stdctx.Context, string) ([]platform.GroupMemberInfo, error)
}

func (s *groupInfoSenderStub) GetGroupInfo(stdctx.Context, string) (platform.GroupInfo, error) {
	return platform.GroupInfo{}, nil
}

func (s *groupInfoSenderStub) GetGroupMemberList(ctx stdctx.Context, groupID string) ([]platform.GroupMemberInfo, error) {
	return s.listFn(ctx, groupID)
}

func (s *groupInfoSenderStub) GetGroupMember(stdctx.Context, string, string) (platform.GroupMemberInfo, error) {
	return platform.GroupMemberInfo{}, nil
}

func (s *groupInfoSenderStub) GetJoinedGroups(stdctx.Context) ([]platform.GroupInfo, error) {
	return nil, nil
}

var _ platform.GroupInfoProvider = (*groupInfoSenderStub)(nil)

func groupEventCtx() *Context {
	evt := newMockEvent(platform.EventKindGroupMessage)
	evt.chat = platform.ChatInfo{ID: "g-1", IsGroup: true}
	return NewContextFromEvent(evt, nil)
}

func TestGetGroupMembers_PropagatesPlatformError(t *testing.T) {
	ctxDownstream := errors.New("upstream 503")
	c := groupEventCtx()
	c.platformSender = &groupInfoSenderStub{listFn: func(stdctx.Context, string) ([]platform.GroupMemberInfo, error) {
		return nil, ctxDownstream
	}}

	_, err := c.GetGroupMembers()
	require.Error(t, err, "平台错误必须透出而不是被折叠成 false")
	assert.ErrorIs(t, err, ctxDownstream)

	// 兼容 API 仍返回 false，但错误源可见
	_, ok := c.TryGetGroupMembers()
	assert.False(t, ok)
}

func TestGetGroupMembers_UnsupportedIsDistinct(t *testing.T) {
	c := groupEventCtx()
	c.platformSender = &platform.NoopSender{} // 未实现 GroupInfoProvider

	_, err := c.GetGroupMembers()
	assert.ErrorIs(t, err, ErrGroupInfoUnsupported)

	// 非群组会话同样归为"不支持"
	direct := NewContextFromEvent(newMockEvent(platform.EventKindPrivateMessage), &platform.NoopSender{})
	_, err = direct.GetGroupMembers()
	assert.ErrorIs(t, err, ErrGroupInfoUnsupported)
}

func TestGetGroupMembers_Success(t *testing.T) {
	c := groupEventCtx()
	c.platformSender = &groupInfoSenderStub{listFn: func(stdctx.Context, string) ([]platform.GroupMemberInfo, error) {
		return []platform.GroupMemberInfo{{UserID: "u1"}}, nil
	}}

	members, err := c.GetGroupMembers()
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, "u1", members[0].UserID)

	got, ok := c.TryGetGroupMembers()
	assert.True(t, ok)
	assert.Len(t, got, 1)
}

func TestEventTypeConstantsMatchPlatform(t *testing.T) {
	// 常量必须是 platform.EventKind 的派生物，而不是字面量副本
	assert.Equal(t, string(platform.EventKindPrivateMessage), EventPrivate)
	assert.Equal(t, string(platform.EventKindGroupMessage), EventGroup)
	assert.Equal(t, string(platform.EventKindGuildMessage), EventGuild)
	assert.Equal(t, string(platform.EventKindNotice), EventNotice)
	assert.Equal(t, string(platform.EventKindInteraction), EventInteraction)
	assert.Equal(t, string(platform.EventKindReaction), EventReaction)
	assert.Equal(t, string(platform.EventKindMemberJoin), EventMemberJoin)
	assert.Equal(t, string(platform.EventKindMemberLeave), EventMemberLeave)
	assert.Equal(t, string(platform.EventKindMessageUpdate), EventMessageUpdate)
	assert.Equal(t, string(platform.EventKindMessageDelete), EventMessageDelete)
}
