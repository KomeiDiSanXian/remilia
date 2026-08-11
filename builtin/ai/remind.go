// Package ai remind.go — 对话式定时提醒。
//
// 用户可在 AI 会话中设置定时提醒（如 "/ai remind 5分钟 去喝水"），
// 到期后机器人主动推送提醒消息到原会话（群/私聊）。
//
// 能力对齐 QQ 官方 OpenClaw 插件的 Scheduled Push：
//   - /ai remind <时长> <内容>       — 设置提醒
//   - /ai remind list                — 列出本会话的活跃提醒
//   - /ai remind cancel <ID>         — 取消指定提醒
//
// 主动推送依赖平台 Sender 实现 platform.SessionNotifier（QQ 已实现）；
// 平台不支持时设置提醒仍成功，但到期推送会静默失败（日志记录）。
package ai

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// reminder 一条定时提醒。
type reminder struct {
	// ID 提醒唯一标识（按会话内序号生成）。
	ID string
	// Text 提醒内容。
	Text string
	// At 触发时间。
	At time.Time
	// ChatID 目标会话 ID（群或私聊）。
	ChatID string
	// IsGroup 是否为群聊会话。
	IsGroup bool
	// sender 主动推送用的 Sender（注册时捕获）。
	sender platform.Sender
	// cancel 取消定时器的函数（time.After 协程）。
	cancel context.CancelFunc
}

// reminderManager 管理全部定时提醒（进程内存储，重启后失效）。
type reminderManager struct {
	mu    sync.Mutex
	items map[string]*reminder
	// seq 按会话递增的序号，用于生成提醒 ID。
	seq map[string]int
}

func newReminderManager() *reminderManager {
	return &reminderManager{
		items: make(map[string]*reminder),
		seq:   make(map[string]int),
	}
}

// nextID 生成某会话的下一个提醒 ID（"R1"、"R2" …）。
func (m *reminderManager) nextID(chatID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq[chatID]++
	return "R" + strconv.Itoa(m.seq[chatID])
}

// add 注册一条提醒并启动定时器。
func (m *reminderManager) add(r *reminder) {
	m.mu.Lock()
	m.items[r.ID] = r
	m.mu.Unlock()
}

// remove 移除提醒并取消定时器，返回是否命中。
func (m *reminderManager) remove(id string) bool {
	m.mu.Lock()
	r, ok := m.items[id]
	if ok {
		delete(m.items, id)
	}
	m.mu.Unlock()
	if ok && r.cancel != nil {
		r.cancel()
	}
	return ok
}

// list 返回某会话的全部提醒（按触发时间升序）。
func (m *reminderManager) list(chatID string) []*reminder {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*reminder
	for _, r := range m.items {
		if r.ChatID == chatID {
			out = append(out, r)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].At.Before(out[j-1].At); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// stopAll 停止全部提醒（插件 Teardown 时调用）。
func (m *reminderManager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.items {
		if r.cancel != nil {
			r.cancel()
		}
	}
	m.items = make(map[string]*reminder)
}

// handleRemindCommand 处理 /ai remind 子命令。
func (p *Plugin) handleRemindCommand(ctx *eventctx.Context, rest string) error {
	parts := strings.SplitN(strings.TrimSpace(rest), " ", 2)
	subCmd := strings.ToLower(parts[0])

	switch subCmd {
	case "list", "ls":
		return p.handleRemindList(ctx)
	case "cancel", "rm", "del", "delete":
		if len(parts) < 2 {
			ctx.ReplyText("❌ 请指定提醒 ID，用法：`/ai remind cancel <ID>`（ID 见 `/ai remind list`）")
			return nil
		}
		return p.handleRemindCancel(ctx, strings.TrimSpace(parts[1]))
	case "":
		ctx.ReplyText(remindHelpText(p.cfg.TriggerCmd))
		return nil
	default:
		// 默认视为设置提醒：<时长> <内容>
		duration, content, ok := parseRemindArgs(rest)
		if !ok {
			ctx.ReplyText(remindHelpText(p.cfg.TriggerCmd))
			return nil
		}
		return p.handleRemindAdd(ctx, duration, content)
	}
}

func remindHelpText(triggerCmd string) string {
	return fmt.Sprintf(`⏰ **定时提醒**

  `+"`%s remind <时长> <内容>`"+`   — 设置提醒（如 `+"`%s remind 5分钟 去喝水`"+`）
  `+"`%s remind list`"+`            — 列出本会话的提醒
  `+"`%s remind cancel <ID>`"+`     — 取消提醒

支持时长：秒/分钟/小时/天（如 30秒、5分钟、1小时、2天；也支持 30s、5m、1h、2d）`, triggerCmd, triggerCmd, triggerCmd, triggerCmd)
}

// parseRemindArgs 解析 "5分钟 去喝水" → (5m, "去喝水")。
// 支持 "30秒/30s"、"5分钟/5分/5m"、"1小时/1h"、"2天/2d" 等常见写法。
func parseRemindArgs(rest string) (time.Duration, string, bool) {
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return 0, "", false
	}
	d, err := parseRemindDuration(fields[0])
	if err != nil {
		return 0, "", false
	}
	content := strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
	if content == "" {
		return 0, "", false
	}
	return d, content, true
}

// parseRemindDuration 解析提醒时长字符串。
//
// 支持：
//   - 英文缩写：30s、5m、1h、2d（及 30sec/5min/1hour/2days）
//   - 中文：30秒、5分钟、5分、1小时、1时、2天、2日
//   - 纯数字：视为分钟（"5" → 5 分钟）
func parseRemindDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// 纯数字 → 分钟
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("duration must be positive: %q", s)
		}
		return time.Duration(n) * time.Minute, nil
	}

	type unit struct {
		names  []string
		amount time.Duration
	}
	units := []unit{
		{[]string{"天", "日", "d", "days", "day"}, 24 * time.Hour},
		{[]string{"小时", "时", "h", "hours", "hour"}, time.Hour},
		{[]string{"分钟", "分", "m", "mins", "min", "minutes", "minute"}, time.Minute},
		{[]string{"秒", "s", "sec", "secs", "seconds", "second"}, time.Second},
	}
	for _, u := range units {
		for _, name := range u.names {
			if strings.HasSuffix(s, name) {
				numStr := strings.TrimSuffix(s, name)
				n, err := strconv.Atoi(numStr)
				if err != nil || n <= 0 {
					return 0, fmt.Errorf("invalid duration: %q", s)
				}
				return time.Duration(n) * u.amount, nil
			}
		}
	}
	return 0, fmt.Errorf("unsupported duration format: %q", s)
}

// handleRemindAdd 设置一条定时提醒，到期后主动推送到原会话。
func (p *Plugin) handleRemindAdd(ctx *eventctx.Context, duration time.Duration, content string) error {
	if p.reminders == nil {
		p.reminders = newReminderManager()
	}

	chat := ctx.GetChatInfo()
	sender := ctx.GetPlatformSender()
	if sender == nil {
		ctx.ReplyText("❌ 无法获取平台发送器，提醒不可用")
		return nil
	}

	remindCtx, cancel := context.WithCancel(p.lifecycleCtx)
	r := &reminder{
		ID:      p.reminders.nextID(chat.ID),
		Text:    content,
		At:      time.Now().Add(duration),
		ChatID:  chat.ID,
		IsGroup: chat.IsGroup,
		sender:  sender,
		cancel:  cancel,
	}
	p.reminders.add(r)

	// 到期推送（协程，受插件生命周期与 cancel 控制）
	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-remindCtx.Done():
			return
		case <-timer.C:
			p.fireReminder(r)
		}
	}()

	ctx.ReplyText(fmt.Sprintf("⏰ 已设置提醒：%s（%s 后触发，ID: %s）", content, formatRemindDuration(duration), r.ID))
	return nil
}

// fireReminder 触发提醒：通过 SessionNotifier（如 QQ）主动推送，
// 平台不支持主动推送时回退 Sender.Send 尝试。
func (p *Plugin) fireReminder(r *reminder) {
	if r.sender == nil {
		logger.Warnf("[AI] Reminder %s fire skipped: no sender", r.ID)
		return
	}
	msg := platform.TextMessage("⏰ 提醒：" + r.Text)
	pushCtx, cancel := context.WithTimeout(p.lifecycleCtx, 10*time.Second)
	defer cancel()

	// 优先 SessionNotifier（主动推送，不依赖事件上下文）
	if sn, ok := r.sender.(platform.SessionNotifier); ok {
		var err error
		if r.IsGroup {
			err = sn.NotifyGroup(pushCtx, r.ChatID, msg)
		} else {
			err = sn.NotifyUser(pushCtx, r.ChatID, msg)
		}
		if err != nil {
			logger.Warnf("[AI] Reminder %s push failed: %v", r.ID, err)
		}
		return
	}

	// 回退：普通 Send（依赖 ChatInfo 路由）
	_, err := r.sender.Send(pushCtx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: r.ChatID, IsGroup: r.IsGroup},
		Message: msg,
	})
	if err != nil {
		logger.Warnf("[AI] Reminder %s send fallback failed: %v", r.ID, err)
	}
}

// handleRemindList 列出本会话的活跃提醒。
func (p *Plugin) handleRemindList(ctx *eventctx.Context) error {
	if p.reminders == nil {
		p.reminders = newReminderManager()
	}
	items := p.reminders.list(ctx.GetChatInfo().ID)
	if len(items) == 0 {
		ctx.ReplyText("⏰ 当前会话没有活跃的提醒")
		return nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("⏰ **活跃提醒 (%d)**\n\n", len(items)))
	for _, r := range items {
		fmt.Fprintf(&b, "  - `%s` — %s（%s 后触发）\n", r.ID, r.Text, formatRemindDuration(time.Until(r.At)))
	}
	b.WriteString("\n用 `/ai remind cancel <ID>` 取消提醒")
	ctx.ReplyText(b.String())
	return nil
}

// handleRemindCancel 取消指定提醒。
func (p *Plugin) handleRemindCancel(ctx *eventctx.Context, id string) error {
	if p.reminders == nil {
		ctx.ReplyText("❌ 没有可取消的提醒")
		return nil
	}
	if p.reminders.remove(id) {
		ctx.ReplyText(fmt.Sprintf("✅ 已取消提醒 `%s`", id))
	} else {
		ctx.ReplyText(fmt.Sprintf("❌ 未找到提醒 `%s`（ID 见 `/ai remind list`）", id))
	}
	return nil
}

// formatRemindDuration 格式化剩余时长（"5分钟"、"1小时30分"、"2天"）。
func formatRemindDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	d -= time.Duration(mins) * time.Minute
	secs := int(d / time.Second)

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d天", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d小时", hours))
	}
	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%d分钟", mins))
	}
	if secs > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d秒", secs))
	}
	return strings.Join(parts, "")
}
