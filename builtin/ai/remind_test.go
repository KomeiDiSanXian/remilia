package ai

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemindDuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"30s", 30 * time.Second, true},
		{"30秒", 30 * time.Second, true},
		{"5m", 5 * time.Minute, true},
		{"5分钟", 5 * time.Minute, true},
		{"5分", 5 * time.Minute, true},
		{"1h", time.Hour, true},
		{"1小时", time.Hour, true},
		{"1时", time.Hour, true},
		{"2d", 48 * time.Hour, true},
		{"2天", 48 * time.Hour, true},
		{"2日", 48 * time.Hour, true},
		{"90sec", 90 * time.Second, true},
		{"10min", 10 * time.Minute, true},
		{"2hour", 2 * time.Hour, true},
		{"3days", 72 * time.Hour, true},
		{"5", 5 * time.Minute, true}, // 纯数字 = 分钟
		{"", 0, false},
		{"abc", 0, false},
		{"-5m", 0, false},
		{"0", 0, false},
		{"5x", 0, false},
		{"分钟", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseRemindDuration(tc.in)
			if !tc.ok {
				assert.Error(t, err, "expected error for %q", tc.in)
				return
			}
			require.NoError(t, err, "unexpected error for %q", tc.in)
			assert.Equal(t, tc.want, got, "duration for %q", tc.in)
		})
	}
}

func TestParseRemindArgs(t *testing.T) {
	tests := []struct {
		in      string
		wantDur time.Duration
		wantMsg string
		ok      bool
	}{
		{"5分钟 去喝水", 5 * time.Minute, "去喝水", true},
		{"30秒 检查服务器", 30 * time.Second, "检查服务器", true},
		{"1小时 开会", time.Hour, "开会", true},
		{"2天 交报告", 48 * time.Hour, "交报告", true},
		{"5分钟", 0, "", false},    // 缺内容
		{"5分钟 ", 0, "", false},   // 内容为空白
		{"abc 内容", 0, "", false}, // 时长非法
		{"", 0, "", false},       // 空参数
		{"5分钟  多空格内容", 5 * time.Minute, "多空格内容", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			d, msg, ok := parseRemindArgs(tc.in)
			if !tc.ok {
				assert.False(t, ok, "expected failure for %q", tc.in)
				return
			}
			require.True(t, ok, "unexpected failure for %q", tc.in)
			assert.Equal(t, tc.wantDur, d)
			assert.Equal(t, tc.wantMsg, msg)
		})
	}
}

func TestFormatRemindDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{5 * time.Minute, "5分钟"},
		{90 * time.Minute, "1小时30分钟"},
		{2 * 24 * time.Hour, "2天"},
		{45 * time.Second, "45秒"},
		{1*time.Hour + 5*time.Minute, "1小时5分钟"},
		{0, "0秒"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, formatRemindDuration(tc.in), "duration %v", tc.in)
	}
}

func TestReminderManagerLifecycle(t *testing.T) {
	m := newReminderManager()
	chatID := "chat_1"

	r1 := &reminder{ID: m.nextID(chatID), Text: "a", At: time.Now().Add(2 * time.Minute), ChatID: chatID}
	r2 := &reminder{ID: m.nextID(chatID), Text: "b", At: time.Now().Add(1 * time.Minute), ChatID: chatID}
	m.add(r1)
	m.add(r2)

	// nextID 生成 R1、R2
	assert.Equal(t, "R1", r1.ID)
	assert.Equal(t, "R2", r2.ID)

	// list 按触发时间升序
	items := m.list(chatID)
	require.Len(t, items, 2)
	assert.Equal(t, "b", items[0].Text)
	assert.Equal(t, "a", items[1].Text)

	// remove 命中并删除
	assert.True(t, m.remove("R1"))
	assert.Len(t, m.list(chatID), 1)
	assert.False(t, m.remove("R9"))

	// stopAll 清空
	m.add(&reminder{ID: m.nextID(chatID), Text: "c", At: time.Now().Add(time.Minute), ChatID: chatID})
	m.stopAll()
	assert.Len(t, m.list(chatID), 0)
}

// fakeNotifierSender 同时实现 platform.Sender 与 platform.SessionNotifier，
// 记录主动推送调用，用于验证 fireReminder。
type fakeNotifierSender struct {
	mu      sync.Mutex
	groupID string
	userID  string
	text    string
}

func (s *fakeNotifierSender) Send(_ context.Context, _ platform.SendRequest) (platform.SendResult, error) {
	return platform.SendResult{}, nil
}
func (s *fakeNotifierSender) NotifyGroup(_ context.Context, groupID string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groupID = groupID
	s.text = msg.Text
	return nil
}
func (s *fakeNotifierSender) NotifyUser(_ context.Context, userID string, msg platform.OutboundMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userID = userID
	s.text = msg.Text
	return nil
}

func TestFireReminder_GroupNotify(t *testing.T) {
	sender := &fakeNotifierSender{}
	p := &Plugin{lifecycleCtx: context.Background()}
	r := &reminder{
		ID:      "R1",
		Text:    "去喝水",
		ChatID:  "group_1",
		IsGroup: true,
		sender:  sender,
	}
	p.fireReminder(r)
	sender.mu.Lock()
	defer sender.mu.Unlock()
	assert.Equal(t, "group_1", sender.groupID)
	assert.Equal(t, "⏰ 提醒：去喝水", sender.text)
}

func TestFireReminder_UserNotify(t *testing.T) {
	sender := &fakeNotifierSender{}
	p := &Plugin{lifecycleCtx: context.Background()}
	r := &reminder{
		ID:      "R2",
		Text:    "下班打卡",
		ChatID:  "user_1",
		IsGroup: false,
		sender:  sender,
	}
	p.fireReminder(r)
	sender.mu.Lock()
	defer sender.mu.Unlock()
	assert.Equal(t, "user_1", sender.userID)
	assert.Equal(t, "⏰ 提醒：下班打卡", sender.text)
}

func TestFireReminder_NoSenderNoPanic(t *testing.T) {
	p := &Plugin{lifecycleCtx: context.Background()}
	r := &reminder{ID: "R3", Text: "x", ChatID: "c", IsGroup: true}
	require.NotPanics(t, func() { p.fireReminder(r) })
}
