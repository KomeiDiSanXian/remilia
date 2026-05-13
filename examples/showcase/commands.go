package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/acl"
	"github.com/KomeiDiSanXian/remilia/builtin/broadcast"
	"github.com/KomeiDiSanXian/remilia/builtin/cooldown"
	"github.com/KomeiDiSanXian/remilia/builtin/i18n"
	"github.com/KomeiDiSanXian/remilia/builtin/job"
	"github.com/KomeiDiSanXian/remilia/builtin/sendqueue"
	"github.com/KomeiDiSanXian/remilia/builtin/stats"
	subscriptionpkg "github.com/KomeiDiSanXian/remilia/builtin/subscription"
	"github.com/KomeiDiSanXian/remilia/builtin/verifycode"
	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// getPlugin 从容器获取插件实例，泛型辅助。
func getPlugin[T any](pm *plugin.Manager, name string) T {
	v, ok := pm.GetContainer().Get(name)
	if !ok {
		panic("plugin " + name + " not found")
	}
	return v.(T)
}

// commandPlugin 创建包含所有演示命令的插件描述符。
// 每个命令 handler 在运行时从容器获取插件实例（而非闭包捕获），
// 避免 Setup 阶段依赖具体插件引用。
func commandPlugin(pm *plugin.Manager) *plugin.Descriptor {
	return &plugin.Descriptor{
		Name:    "commands",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Description: "showcase command set",
			Category:    "showcase",
		},
		Deps: []string{
			"cooldown", "stats", "i18n", "verifycode",
			"broadcast", "sendqueue", "subscription", "job",
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			reg := ctx.Reg

			// /ping — 连通性测试
			reg.RegisterCommand("", "/ping").
				SetDefinition(&command.Definition{Name: "ping", Description: "Pong!", Category: "tools"}).
				Handle(func(c *eventctx.Context) error { return replyCtx(c, "Pong!") })

			// /status — Bot 状态
			reg.RegisterCommand("", "/status").
				SetDefinition(&command.Definition{Name: "status", Description: "Bot status", Category: "tools"}).
				Handle(func(c *eventctx.Context) error {
					sp := getPlugin[*stats.Plugin](pm, "stats")
					top := sp.TopCommands(3)
					var msg strings.Builder
					msg.WriteString(fmt.Sprintf("total=%d top3:", sp.TotalMessages()))
					for _, t := range top {
						msg.WriteString(fmt.Sprintf(" %s*%d", t.Command, t.Count))
					}
					return replyCtx(c, msg.String())
				})

			// /daily — 冷却演示
			reg.RegisterCommand("", "/daily").
				SetDefinition(&command.Definition{Name: "daily", Description: "每日签到（cooldown 演示）", Category: "fun"}).
				Handle(func(c *eventctx.Context) error {
					cd := getPlugin[*cooldown.Plugin](pm, "cooldown")
					uid := c.GetUserID()
					if !cd.Allow(uid, "daily", 24*time.Hour) {
						r := cd.Remaining(uid, "daily", 24*time.Hour)
						return replyCtx(c, fmt.Sprintf("冷却中: %s 后可用", r.Round(time.Second)))
					}
					return replyCtx(c, "签到成功！")
				})

			// /lang — i18n 语言切换
			reg.RegisterCommand("", "/lang").
				SetDefinition(&command.Definition{
					Name: "lang", Description: "切换语言（i18n 演示）", Category: "settings",
					Arguments: []*command.Argument{{Name: "locale", Type: command.ArgTypeString, Required: true}},
					Examples:  []string{"/lang zh-CN", "/lang en"},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					locale := args.Get(0)
					if locale == "" {
						return replyCtx(c, "用法: /lang <zh-CN|en>")
					}
					i18n := getPlugin[*i18n.Plugin](pm, "i18n")
					i18n.SetLocale(c, locale)
					return replyCtx(c, "语言已切换: "+locale)
				})

			// /greet — i18n 问候语
			reg.RegisterCommand("", "/greet").
				SetDefinition(&command.Definition{Name: "greet", Description: "i18n 问候", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					i18n := getPlugin[*i18n.Plugin](pm, "i18n")
					return replyCtx(c, i18n.T(c, "welcome", map[string]any{"name": c.GetUserID()}))
				})

			// /verify — 验证码兑换
			reg.RegisterCommand("", "/verify").
				SetDefinition(&command.Definition{
					Name: "verify", Description: "兑换验证码", Category: "access",
					Arguments: []*command.Argument{{Name: "code", Type: command.ArgTypeString, Required: true}},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					code := args.Get(0)
					if code == "" {
						return replyCtx(c, "用法: /verify <code>")
					}
					vp := getPlugin[*verifycode.Plugin](pm, "verifycode")
					role, err := vp.Verify(c.GetUserID(), code)
					if err != nil {
						return replyCtx(c, "验证失败: "+err.Error())
					}
					return replyCtx(c, "已授予角色: "+role)
				})

			// /aclcheck — ACL 状态
			reg.RegisterCommand("", "/aclcheck").
				SetDefinition(&command.Definition{Name: "aclcheck", Description: "ACL 状态", Category: "security"}).
				Handle(func(c *eventctx.Context) error {
					p := getPlugin[*acl.Plugin](pm, "acl")
					return replyCtx(c, fmt.Sprintf("模式=%s 用户=%s 允许=%v", p.GetMode(), c.GetUserID(), p.IsAllowed(c.GetUserID())))
				})

			// /broadcast — 广播消息
			reg.RegisterCommand("", "/broadcast").
				SetDefinition(&command.Definition{
					Name: "broadcast", Description: "广播消息演示", Category: "demo",
					Arguments: []*command.Argument{{Name: "msg", Type: command.ArgTypeString, Required: true}},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					text := args.Get(0)
					if text == "" {
						return replyCtx(c, "用法: /broadcast <消息>")
					}
					bc := getPlugin[*broadcast.Plugin](pm, "broadcast")
					bc.SetSender(c.GetPlatformSender())
					groups := bc.ListGroupSubscribers()
					if len(groups) == 0 {
						return replyCtx(c, "暂无订阅群，使用 /bcsub 订阅")
					}
					result := bc.BroadcastToGroupsWithContext(c.Context(), groups, platform.TextMessage(text))
					return replyCtx(c, fmt.Sprintf("广播完成: 总数=%d 成功=%d 失败=%d", result.Total, result.Success, result.Failed))
				})

			// /bcsub / /bcunsub — 广播订阅管理
			reg.RegisterCommand("", "/bcsub").
				SetDefinition(&command.Definition{Name: "bcsub", Description: "订阅广播", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					bc := getPlugin[*broadcast.Plugin](pm, "broadcast")
					bc.SubscribeGroup(c.GetUserID() + "_group")
					return replyCtx(c, "已订阅")
				})
			reg.RegisterCommand("", "/bcunsub").
				SetDefinition(&command.Definition{Name: "bcunsub", Description: "取消广播订阅", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					bc := getPlugin[*broadcast.Plugin](pm, "broadcast")
					bc.UnsubscribeGroup(c.GetUserID() + "_group")
					return replyCtx(c, "已取消订阅")
				})

			// /enqueue — 异步消息队列
			reg.RegisterCommand("", "/enqueue").
				SetDefinition(&command.Definition{
					Name: "enqueue", Description: "异步消息队列演示", Category: "demo",
					Arguments: []*command.Argument{{Name: "msg", Type: command.ArgTypeString, Required: true}},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					text := args.Get(0)
					if text == "" {
						return replyCtx(c, "用法: /enqueue <消息>")
					}
					sq := getPlugin[*sendqueue.Plugin](pm, "sendqueue")
					sq.SetDefaultSender(c.GetPlatformSender())
					chat := platform.ChatInfo{ID: c.GetUserID(), IsGroup: false}
					if err := sq.Enqueue(chat, platform.TextMessage("[async] "+text), nil); err != nil {
						return replyCtx(c, "入队失败: "+err.Error())
					}
					return replyCtx(c, "消息已加入发送队列")
				})

			// /subscribe /unsubscribe /mysubs — 订阅框架
			reg.RegisterCommand("", "/subscribe").
				SetDefinition(&command.Definition{Name: "subscribe", Description: "订阅 demo 数据源", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					sub := getPlugin[*subscriptionpkg.PluginHandle](pm, "subscription")
					id, err := sub.Manager().Subscribe("demo", "showcase",
						subscriptionpkg.Target{ChatID: c.GetUserID(), IsGroup: false})
					if err != nil {
						return replyCtx(c, "订阅失败: "+err.Error())
					}
					return replyCtx(c, "订阅成功 id="+id)
				})

			reg.RegisterCommand("", "/unsubscribe").
				SetDefinition(&command.Definition{
					Name: "unsubscribe", Description: "取消订阅", Category: "demo",
					Arguments: []*command.Argument{{Name: "id", Type: command.ArgTypeString, Required: true}},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					id := args.Get(0)
					if id == "" {
						return replyCtx(c, "用法: /unsubscribe <id>")
					}
					sub := getPlugin[*subscriptionpkg.PluginHandle](pm, "subscription")
					if err := sub.Manager().Unsubscribe(id); err != nil {
						return replyCtx(c, "取消失败: "+err.Error())
					}
					return replyCtx(c, "已取消 id="+id)
				})

			reg.RegisterCommand("", "/mysubs").
				SetDefinition(&command.Definition{Name: "mysubs", Description: "查看订阅列表", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					sub := getPlugin[*subscriptionpkg.PluginHandle](pm, "subscription")
					subs := sub.Manager().ListSubscriptions(c.GetUserID())
					if len(subs) == 0 {
						return replyCtx(c, "暂无订阅")
					}
					var sb strings.Builder
					for _, s := range subs {
						sb.WriteString(fmt.Sprintf("id=%s source=%s\n", s.ID, s.SourceName))
					}
					return replyCtx(c, sb.String())
				})

			// /runjob — 后台作业
			reg.RegisterCommand("", "/runjob").
				SetDefinition(&command.Definition{Name: "runjob", Description: "后台作业演示", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					runner := getPlugin[*job.Plugin](pm, "job")
					uid := c.GetUserID()
					jid := runner.Once("showcase-task",
						func(jctx context.Context) error {
							logger.Infof("[job] showcase-task for user=%s done", uid)
							return nil
						},
						job.WithDelay(3*time.Second),
						job.WithOnDone(func(info job.Info) {
							logger.Infof("[job] %s finished", info.Name)
						}),
					)
					return replyCtx(c, fmt.Sprintf("作业已提交: id=%s", jid))
				})

			// /jobretrydemo — 带重试的后台作业
			reg.RegisterCommand("", "/jobretrydemo").
				SetDefinition(&command.Definition{Name: "jobretrydemo", Description: "重试作业演示", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					runner := getPlugin[*job.Plugin](pm, "job")
					attempt := 0
					jid := runner.Retry("retry-demo",
						func(jctx context.Context) error {
							attempt++
							if attempt < 3 {
								return fmt.Errorf("模拟失败 (第 %d 次)", attempt)
							}
							return nil
						},
						job.WithMaxRetries(3),
						job.WithExponentialBackoff(200*time.Millisecond, 2*time.Second),
					)
					return replyCtx(c, fmt.Sprintf("重试作业已提交: id=%s", jid))
				})

			return nil, nil
		},
	}
}
