package mock

import (
	stdctx "context"
	"slices"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

// SenderCall records a single sender method call for assertions.
type SenderCall struct {
	Method    string
	ChatID    string
	MessageID string
	Msg       platform.OutboundMessage
	Emoji     platform.Emoji
	Error     error
}

// SenderOption configures a MockSender.
type SenderOption func(*MockSender)

// WithSendError makes Send() return the given error.
func WithSendError(err error) SenderOption {
	return func(s *MockSender) { s.sendErr = err }
}

// WithSendResult sets the result returned by Send().
func WithSendResult(r platform.SendResult) SenderOption {
	return func(s *MockSender) { s.sendResult = r }
}

// WithEditError makes Edit() return the given error.
func WithEditError(err error) SenderOption {
	return func(s *MockSender) { s.editErr = err }
}

// WithDeleteError makes Delete() return the given error.
func WithDeleteError(err error) SenderOption {
	return func(s *MockSender) { s.deleteErr = err }
}

// WithSkipValidation 关闭 MockSender.Send 的 SendRequest 校验。
//
// 默认情况下 MockSender 与真实 Sender 一样先执行 req.Validate()，
// 以免非法请求在测试里一路绿灯、到生产才失败。
// 少数测试需要刻意构造非法请求来验证上游逻辑时，用这个选项关掉校验。
func WithSkipValidation() SenderOption {
	return func(s *MockSender) { s.skipValidation = true }
}

// MockSender is a full mock implementation of platform.Sender
// plus all optional sender interfaces: MessageEditor, MessageDeleter,
// ReactionSender, TypingNotifier, GroupManager, InvitationHandler,
// AutoModerator, GroupInfoProvider, AvatarProvider, SessionNotifier.
//
// All calls are recorded in Calls for test assertions.
type MockSender struct {
	sendResult platform.SendResult
	sendErr    error
	editErr    error
	deleteErr  error

	// skipValidation 关闭 Send 的 req.Validate() 校验（见 WithSkipValidation）。
	skipValidation bool

	mu    sync.Mutex
	Calls []SenderCall
}

// NewSender creates a MockSender with default values.
func NewSender(opts ...SenderOption) *MockSender {
	s := &MockSender{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Send records the call and returns configured result/error.
//
// 与真实 Sender 一致，先执行 req.Validate()。
//
// 之所以必须校验：platform.Sender 的契约要求实现方对非法请求返回
// errutil.ErrNoChatInfo 等错误，milky / telegram / satori 等真实实现
// 都在入口调用 Validate。此前这个替身不校验，于是"漏填 Target.ID"
// 或"URL 与 Data 同时设置"这类错误在测试里一路绿灯，
// 到了生产环境才静默失败——正好是测试替身最该拦住的一类问题。
//
// 需要保留旧行为（构造非法请求断言其它逻辑）时，用 WithSkipValidation()。
func (s *MockSender) Send(ctx stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	s.mu.Lock()
	// 先记录调用再校验，使 CalledTimes 仍能反映"尝试发送过一次"。
	s.Calls = append(s.Calls, SenderCall{
		Method: "Send",
		ChatID: req.Target.ID,
		Msg:    req.Message,
	})
	skip := s.skipValidation
	result := s.sendResult
	err := s.sendErr
	s.mu.Unlock()

	if !skip {
		if vErr := req.Validate(); vErr != nil {
			return platform.SendResult{}, vErr
		}
	}
	return result, err
}

// Edit records the call (implements MessageEditor).
func (s *MockSender) Edit(ctx stdctx.Context, chatID, messageID string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{
		Method:    "Edit",
		ChatID:    chatID,
		MessageID: messageID,
		Msg:       msg,
	})
	err := s.editErr
	s.mu.Unlock()
	return err
}

// Delete records the call (implements MessageDeleter).
func (s *MockSender) Delete(ctx stdctx.Context, chatID, messageID string) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{
		Method:    "Delete",
		ChatID:    chatID,
		MessageID: messageID,
	})
	err := s.deleteErr
	s.mu.Unlock()
	return err
}

// AddReaction records the call (implements ReactionSender).
func (s *MockSender) AddReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{
		Method:    "AddReaction",
		ChatID:    chatID,
		MessageID: messageID,
		Emoji:     emoji,
	})
	s.mu.Unlock()
	return nil
}

// RemoveReaction records the call (implements ReactionSender).
func (s *MockSender) RemoveReaction(ctx stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{
		Method:    "RemoveReaction",
		ChatID:    chatID,
		MessageID: messageID,
		Emoji:     emoji,
	})
	s.mu.Unlock()
	return nil
}

// SendTyping records the call (implements TypingNotifier).
func (s *MockSender) SendTyping(ctx stdctx.Context, chatID string) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "SendTyping", ChatID: chatID})
	s.mu.Unlock()
	return nil
}

// --- GroupManager ---

// KickMember records the call (implements GroupManager).
func (s *MockSender) KickMember(ctx stdctx.Context, groupID, userID string, permanent bool) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "KickMember", ChatID: groupID})
	s.mu.Unlock()
	return nil
}

// BanMember records the call (implements GroupManager).
func (s *MockSender) BanMember(ctx stdctx.Context, groupID, userID string, duration time.Duration) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "BanMember", ChatID: groupID})
	s.mu.Unlock()
	return nil
}

// SetAdmin records the call (implements GroupManager).
func (s *MockSender) SetAdmin(ctx stdctx.Context, groupID, userID string, isAdmin bool) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "SetAdmin", ChatID: groupID})
	s.mu.Unlock()
	return nil
}

// --- InvitationHandler ---

func (s *MockSender) AcceptGroupInvite(ctx stdctx.Context, inviteID string) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "AcceptGroupInvite"})
	s.mu.Unlock()
	return nil
}

func (s *MockSender) RejectGroupInvite(ctx stdctx.Context, inviteID, reason string) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "RejectGroupInvite"})
	s.mu.Unlock()
	return nil
}

func (s *MockSender) AcceptFriendRequest(ctx stdctx.Context, requestID string) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "AcceptFriendRequest"})
	s.mu.Unlock()
	return nil
}

func (s *MockSender) RejectFriendRequest(ctx stdctx.Context, requestID, reason string) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "RejectFriendRequest"})
	s.mu.Unlock()
	return nil
}

// --- AutoModerator ---

func (s *MockSender) DeleteMemberMessage(ctx stdctx.Context, groupID, messageID string) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "DeleteMemberMessage", ChatID: groupID})
	s.mu.Unlock()
	return nil
}

func (s *MockSender) MuteAll(ctx stdctx.Context, groupID string, mute bool) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "MuteAll", ChatID: groupID})
	s.mu.Unlock()
	return nil
}

// --- GroupInfoProvider ---

func (s *MockSender) GetGroupInfo(ctx stdctx.Context, groupID string) (platform.GroupInfo, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "GetGroupInfo", ChatID: groupID})
	s.mu.Unlock()
	return platform.GroupInfo{ID: groupID, Name: "mock group"}, nil
}

func (s *MockSender) GetGroupMemberList(ctx stdctx.Context, groupID string) ([]platform.GroupMemberInfo, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "GetGroupMemberList", ChatID: groupID})
	s.mu.Unlock()
	return nil, nil
}

func (s *MockSender) GetGroupMember(ctx stdctx.Context, groupID, userID string) (platform.GroupMemberInfo, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "GetGroupMember", ChatID: groupID})
	s.mu.Unlock()
	return platform.GroupMemberInfo{}, nil
}

func (s *MockSender) GetJoinedGroups(ctx stdctx.Context) ([]platform.GroupInfo, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "GetJoinedGroups"})
	s.mu.Unlock()
	return nil, nil
}

// --- AvatarProvider ---

func (s *MockSender) GetUserAvatarURL(ctx stdctx.Context, userID string) (string, error) {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "GetUserAvatarURL"})
	s.mu.Unlock()
	return "https://example.com/avatar.png", nil
}

// --- SessionNotifier ---

func (s *MockSender) NotifyUser(ctx stdctx.Context, userID string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "NotifyUser", ChatID: userID})
	s.mu.Unlock()
	return nil
}

func (s *MockSender) NotifyGroup(ctx stdctx.Context, groupID string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	s.Calls = append(s.Calls, SenderCall{Method: "NotifyGroup", ChatID: groupID})
	s.mu.Unlock()
	return nil
}

// CalledTimes returns how many times a method was called.
func (s *MockSender) CalledTimes(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// ResetCalls clears recorded calls.
func (s *MockSender) ResetCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = nil
}

// Snapshot 返回已记录调用的副本，可安全地并发读取。
//
// 断言调用内容（ChatID / MessageID / Msg / Emoji）时请使用本方法，
// 不要直接读取导出字段 Calls：所有写入都在 s.mu 保护下进行，而直接读取
// Calls 不持锁，与内部的 append 构成数据竞争（-race 必报）。不加 -race 时
// 还可能读到 append 扩容前的旧切片头，表现为"少了几次调用"的偶发失败。
// 由于这是给使用者写测试用的替身，这类竞争最终会落在他们的测试套件里。
func (s *MockSender) Snapshot() []SenderCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.Calls)
}
