// milky-bot demonstrates how to connect to a Milky QQ protocol server
// using the remilia bot framework.
//
// Prerequisites:
//  1. A running Milky server (e.g., NapCat, LLOneBot, or any Milky-compatible implementation)
//     Refer to https://milky.ntqqrev.org/ for supported implementations.
//  2. Configure the Milky server to listen on an accessible address.
//
// Usage:
//
//	MILKY_BASE_URL=http://127.0.0.1:6700 \
//	MILKY_ACCESS_TOKEN=your_token \
//	go run .
package main

import (
	"log"
	"os"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/milky"
)

func main() {
	// ── Init logger ──────────────────────────────────────────────────────────
	if err := logger.Init(logger.Config{
		Level:      "info",
		Console:    true,
		TimeFormat: "2006-01-02 15:04:05",
	}); err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}

	// ── Build Milky adapter ──────────────────────────────────────────────────
	baseURL := getEnv("MILKY_BASE_URL", "http://127.0.0.1:6700")
	token := getEnv("MILKY_ACCESS_TOKEN", "")

	adapter, err := milky.NewAdapter(milky.Config{
		BaseURL:         baseURL,
		AccessToken:     token,
		WorkerCount:     4,
		EventBufferSize: 128,
		ReconnectDelay:  3 * time.Second,
		MaxReconnect:    0, // unlimited — keep reconnecting
		DialTimeout:     10 * time.Second,
		APITimeout:      15 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to create milky adapter: %v", err)
	}

	// ── Build bot ────────────────────────────────────────────────────────────
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		WithName("milky-bot").
		Build()
	if err != nil {
		log.Fatalf("failed to build bot: %v", err)
	}

	// ── Register handlers ────────────────────────────────────────────────────
	registerHandlers(bot.Engine(), adapter)

	// ── Start ────────────────────────────────────────────────────────────────
	logger.Infof("[milky-bot] Connecting to Milky server at %s", baseURL)
	if err := bot.Start(); err != nil {
		logger.WithError(err).Fatal("[milky-bot] Fatal error")
	}
	bot.WaitForShutdown()
	logger.Info("[milky-bot] Stopped")
}

func registerHandlers(eng *engine.Engine, adapter *milky.Adapter) {
	// /ping — basic liveness check
	eng.OnCommand("", "/ping").Handle(func(ctx *eventctx.Context) error {
		ctx.Reply(platform.TextMessage("Pong! 🏓"))
		return nil
	})

	// /echo — echo the message back
	eng.OnCommand("", "/echo").Handle(func(ctx *eventctx.Context) error {
		msg := ctx.GetMessageContent()
		if msg == "" {
			ctx.Reply(platform.TextMessage("请在 /echo 后面输入要回显的内容"))
			return nil
		}
		ctx.Reply(platform.TextMessage("回声: " + msg))
		return nil
	})

	// /info — show bot identity info
	eng.OnCommand("", "/info").Handle(func(ctx *eventctx.Context) error {
		text := "机器人信息:\n"
		if botID := platform.GetBotID(adapter); botID != "" {
			text += "QQ: " + botID + "\n"
		}
		if name := platform.GetBotName(adapter); name != "" {
			text += "昵称: " + name
		}
		ctx.Reply(platform.TextMessage(text))
		return nil
	})

	// /help — list commands
	eng.OnCommand("", "/help").Handle(func(ctx *eventctx.Context) error {
		ctx.Reply(platform.TextMessage(
			"可用命令:\n" +
				"/ping  — 测试机器人是否在线\n" +
				"/echo <内容>  — 回显你的消息\n" +
				"/info  — 查看机器人信息\n" +
				"/help  — 显示此帮助",
		))
		return nil
	})

	// Image example: send an image by URL
	eng.OnCommand("", "/image").Handle(func(ctx *eventctx.Context) error {
		ctx.Reply(platform.ImageMessage("https://gchat.qpic.cn/gchatpic_new/0/0-0-0/0?term=3"))
		return nil
	})

	// GroupAdmin demo: mute sender for 60 seconds (requires bot to be admin)
	eng.OnCommand("", "/mute").Handle(func(ctx *eventctx.Context) error {
		chat := ctx.GetChatInfo()
		if !chat.IsGroup {
			ctx.Reply(platform.TextMessage("此命令仅在群聊中可用"))
			return nil
		}
		if gm, ok := platform.GetGroupManager(adapter); ok {
			senderID := ctx.GetSenderInfo().ID
			if err := gm.BanMember(ctx.Context(), chat.ID, senderID, 60*time.Second); err != nil {
				ctx.Reply(platform.TextMessage("禁言失败: " + err.Error()))
				return nil
			}
			ctx.Reply(platform.TextMessage("已禁言 60 秒"))
			return nil
		}
		ctx.Reply(platform.TextMessage("当前平台不支持群成员管理"))
		return nil
	})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
