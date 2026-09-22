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

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"

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
	items map[string]*reminder // key: chatID + "\x00" + ID（不同会话的同名 ID 不冲突）
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

// reminderKey 生成提醒在管理器中的唯一键（会话内 ID 仅保证本会话唯一）。
func (m *reminderManager) key(chatID, id string) string {
	return chatID + "\x00" + id
}

// add 注册一条提醒并启动定时器。
func (m *reminderManager) add(r *reminder) {
	m.mu.Lock()
	m.items[m.key(r.ChatID, r.ID)] = r
	m.mu.Unlock()
}

// remove 移除指定会话的提醒并取消定时器，返回是否命中。
func (m *reminderManager) remove(chatID, id string) bool {
	m.mu.Lock()
	key := m.key(chatID, id)
	r, ok := m.items[key]
	if ok {
		delete(m.items, key)
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
			p.replyFormatted(ctx, "❌ 请指定提醒 ID，用法：`/ai remind cancel <ID>`（ID 见 `/ai remind list`）")
			return nil
		}
		return p.handleRemindCancel(ctx, strings.TrimSpace(parts[1]))
	case "":
		p.replyFormatted(ctx, p.remindHelpText(p.cfg.TriggerCmd))
		return nil
	default:
		// 默认视为设置提醒：<时长> <内容>
		duration, content, ok := p.parseRemindArgs(rest)
		if !ok {
			p.replyFormatted(ctx, p.remindHelpText(p.cfg.TriggerCmd))
			return nil
		}
		return p.handleRemindAdd(ctx, duration, content)
	}
}

func (a *adminState) remindHelpText(triggerCmd string) string {
	return fmt.Sprintf(`⏰ **定时提醒**

  `+"`%s remind <时长> <内容>`"+`   — 设置提醒（如 `+"`%s remind 5分钟 去喝水`"+`）
  `+"`%s remind list`"+`            — 列出本会话的提醒
  `+"`%s remind cancel <ID>`"+`     — 取消提醒

支持时长：秒/分钟/小时/天（如 30秒、5分钟、1小时、2天；也支持 30s、5m、1h、2d）`, triggerCmd, triggerCmd, triggerCmd, triggerCmd)
}

// parseRemindArgs 解析 "5分钟 去喝水" → (5m, "去喝水")。
// 支持 "30秒/30s"、"5分钟/5分/5m"、"1小时/1h"、"2天/2d" 等常见写法。
func (a *adminState) parseRemindArgs(rest string) (time.Duration, string, bool) {
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return 0, "", false
	}
	d, err := catalog.ParseRemindDuration(fields[0])
	if err != nil {
		return 0, "", false
	}
	content := strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
	if content == "" {
		return 0, "", false
	}
	return d, content, true
}

// addReminder 创建并注册一条定时提醒，返回提醒对象与确认文本。
// 供 /ai remind 子命令与 set_reminder 工具共用。
func (p *Plugin) handleRemindAdd(ctx *eventctx.Context, duration time.Duration, content string) error {
	sender := ctx.GetPlatformSender()
	if sender == nil {
		ctx.ReplyText("❌ 无法获取平台发送器，提醒不可用")
		return nil
	}
	_, confirm := p.addReminder(ctx.GetChatInfo(), sender, duration, content)
	ctx.ReplyText(confirm)
	return nil
}

// addReminder 创建并注册一条定时提醒，返回提醒对象与确认文本。
// 供 /ai remind 子命令与 set_reminder 工具共用。
func (p *Plugin) addReminder(chat platform.ChatInfo, sender platform.Sender, duration time.Duration, content string) (*reminder, string) {
	if p.reminders == nil {
		p.reminders = newReminderManager()
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

	return r, fmt.Sprintf("⏰ 已设置提醒：%s（%s 后触发，ID: %s）", content, catalog.FormatRemindDuration(duration), r.ID)
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
		fmt.Fprintf(&b, "  - `%s` — %s（%s 后触发）\n", r.ID, r.Text, catalog.FormatRemindDuration(time.Until(r.At)))
	}
	b.WriteString("\n用 `/ai remind cancel <ID>` 取消提醒")
	p.replyFormatted(ctx, b.String())
	return nil
}

// handleRemindCancel 取消指定提醒。
func (p *Plugin) handleRemindCancel(ctx *eventctx.Context, id string) error {
	if p.reminders == nil {
		ctx.ReplyText("❌ 没有可取消的提醒")
		return nil
	}
	if p.reminders.remove(ctx.GetChatInfo().ID, id) {
		p.replyFormatted(ctx, fmt.Sprintf("✅ 已取消提醒 `%s`", id))
	} else {
		p.replyFormatted(ctx, fmt.Sprintf("❌ 未找到提醒 `%s`（ID 见 `/ai remind list`）", id))
	}
	return nil
}
