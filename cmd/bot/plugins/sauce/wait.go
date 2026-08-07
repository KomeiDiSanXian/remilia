package sauce

import (
	"fmt"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
)

// ── 等待用户补发图片 ──────────────────────────────────────────────────
//
// 用户发送 /sauce 时未携带图片（且非引用消息）时，注册一个一次性临时
// Matcher 等待同一会话内该用户的下一条消息：
//   - 下一条消息含图片 → 直接以该图片执行检索
//   - 下一条消息不含图片 → 取消等待并提示
//   - 超时（image_wait_timeout，默认 60s）→ 取消等待并提示
//
// 临时 Matcher 由引擎的 TempManager 管理，超时自动清理；手动取消时
// 调用 matcher.Delete() 立即移除。

// imageWait 是单次等待会话的状态。
type imageWait struct {
	matcher *engine.Matcher
	once    sync.Once // 保证取消清理只执行一次
	engines engineSet
}

// beginImageWait 注册等待 Matcher 并提示用户。
func (p *Plugin) beginImageWait(ctx *eventctx.Context, engines engineSet) {
	if p.reg == nil {
		ctx.ReplyError("请在消息中包含图片（如发送图片并在标题中附带 /sauce）")
		return
	}
	event := ctx.GetPlatformEvent()
	if event == nil {
		ctx.ReplyError("无法获取消息内容")
		return
	}
	chatID := event.Chat().ID
	userID := event.Sender().ID
	if chatID == "" {
		ctx.ReplyError("无法获取会话信息")
		return
	}

	w := &imageWait{
		engines: engines,
	}

	// 规则：同一会话 + 同一用户的下一条消息（任意内容）
	m := p.reg.RegisterMatcher("",
		func(c *eventctx.Context) bool { return c.GetChatInfo().ID == chatID },
		eventctx.OnFromUser(userID),
	)
	if m == nil {
		ctx.ReplyError("无法注册图片等待，请直接发送图片并在标题中附带 /sauce")
		return
	}
	w.matcher = m
	// 一次性：命中一次即自动删除（maxUse=1），并带超时清理
	m.SetTempWithMaxUse(1)
	m.SetTempWithTimeout(p.imageWaitTimeout())

	// 超时提示：TempManager 到期清理 matcher 不回调，这里用定时器兜底
	timeout := p.imageWaitTimeout()
	time.AfterFunc(timeout, func() {
		w.cancelOnce(ctx, "等待图片超时，已取消本次搜索（可重新发送 /sauce）")
	})

	// 命中处理：一次性 Matcher 用后自动删除
	m.Handle(func(c *eventctx.Context) error {
		url := findImageURL(c.GetPlatformEvent())
		if url == "" {
			// 发送了非图片消息 → 取消等待
			w.cancelOnce(c, "已取消本次搜索（未收到图片）")
			return nil
		}
		w.cancelOnce(c, "") // 静默清理（成功后不再发取消提示）
		p.runSearch(c, url, w.engines)
		return nil
	})

	ctx.ReplyText(fmt.Sprintf("请发送要搜索的图片（%d 秒内；发送非图片消息即取消）", int(timeout.Seconds())))
}

// cancelOnce 保证取消清理只执行一次；msg 为空时静默清理（成功后调用）。
func (w *imageWait) cancelOnce(ctx *eventctx.Context, msg string) {
	w.once.Do(func() {
		if w.matcher != nil {
			w.matcher.Delete()
		}
		if ctx != nil && msg != "" {
			ctx.ReplyText(msg)
		}
	})
}
