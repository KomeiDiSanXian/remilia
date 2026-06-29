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
	"github.com/KomeiDiSanXian/remilia/platform/qq"
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

			// /mdtest — Markdown 格式测试
			/*
				需要注意，测试发现qq官方bot手机和电脑支持的格式不一样
				同时对于转发的情况，转发后的消息只展示渲染后的纯文本，不显示图片
				目前（2026-05-19），qq官方bot有以下额外的特性：
				- 代码块：手机端点击后可以复制代码内容到剪贴板；电脑端则没有
				- 表格：手机端比电脑端多出复制、保存、全屏功能
				- 电脑端额外支持 html标签，手机端不支持 html标签
				- H3、H4、H5、H6都是一致显示H3
				- 无尺寸图片在手机端可能不显示
				- 脚注在手机端会被移到文本最后一行
				- 普通 加粗 删除 的混合 在手机端可能失效，电脑端暂未观察到失效的情况
				...
				建议开发md相关功能时跑一下这个测试看看格式支持情况
				其他平台的 Markdown 支持情况可能也不一样，开发时需要注意兼容性问题
			*/
			reg.RegisterCommand("", "/mdtest").
				SetDefinition(&command.Definition{Name: "mdtest", Description: "Markdown 格式测试", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					md := `===== 文档支持的格式 =====

# H1 标题
## H2 标题
### H3 标题

**加粗文字**
__下划线加粗__
*斜体文字*
***加粗斜体***
~~删除线~~

欢迎访问：[🔗腾讯网](https://www.qq.com)
原始链接：<https://doc.qq.com>

![图片 #100px #100px](https://resource5-1255303497.cos.ap-guangzhou.myqcloud.com/abcmouse_word_watch/markdown/building.png)

1. 有序列表一
2. 有序列表二
3. 有序列表三

- 无序列表 A
- 无序列表 B
- 无序列表 C

1. 外层有序
    - 嵌套无序一
    - 嵌套无序二
2. 外层有序二
    1. 嵌套有序 1
    2. 嵌套有序 2

> 块引用第一行
> 块引用第二行
> 块引用第三行

分割线上方
***
分割线下方

===== 以下格式文档未列出，预期不支持 =====

行内代码：fmt.Println("hello")

` + "```" + `
代码块
func main() {}
` + "```" + `

| 表格 | 列2 | 列3 |
|------|-----|-----|
| a    | b   | c   |

- [ ] 未完成任务
- [x] 已完成任务

表情简码：:smile: :heart: :+1:

HTML标签：<u>下划线</u> <font color="red">红字</font>

转义：\*不被解析为斜体\*

脚注测试[^1]

[^1]: 脚注内容

#### H4 标题
##### H5 标题
###### H6 标题

> 外层引用
> > 嵌套引用

**加粗中的_斜体_**
~~**加粗删除线**~~
_普通_ **加粗** ~~删除~~ 混合

5. 从 5 开始的有序列表
6. 第二项

无尖括号裸链接：https://www.qq.com

无尺寸图片：
![无尺寸](https://resource5-1255303497.cos.ap-guangzhou.myqcloud.com/abcmouse_word_watch/markdown/building.png)

引用式链接：[点我][ref]
[ref]: https://www.qq.com

分割线变体：---
___
* * *

键盘标签：<kbd>Ctrl+C</kbd>
高亮标签：<mark>高亮</mark>
上标：X<sup>2</sup>
下标：H<sub>2</sub>O

详情折叠：
<details><summary>点我展开</summary>隐藏内容</details>

图片链接：[![图片 #50px #50px](https://resource5-1255303497.cos.ap-guangzhou.myqcloud.com/abcmouse_word_watch/markdown/building.png)](https://www.qq.com)

同一行内的  
换行（行尾两空格）`
					c.Reply(platform.MarkdownMessage(md))
					return nil
				})

			// /arktest — ARK 模板消息测试（QQ 平台专属）
			reg.RegisterCommand("", "/arktest").
				SetDefinition(&command.Definition{
					Name: "arktest", Description: "ARK 模板消息测试（23/24/37）", Category: "demo",
					Arguments: []*command.Argument{{
						Name: "template", Type: command.ArgTypeString, Required: true,
					}},
					Examples: []string{"/arktest 23", "/arktest 24", "/arktest 37"},
				}).
				Handle(func(c *eventctx.Context) error {
					args, _ := command.ParseCommandLine(c.GetMessageContent())
					tpl := args.Get(0)

					var msg platform.OutboundMessage
					switch tpl {
					case "23":
						msg = qq.ApplyExtra(platform.TextMessage(""), qq.MessageExtra{
							Ark: &qq.Ark{TemplateID: 23, KV: []qq.ArkKV{
								{Key: "#DESC#", Value: "今日待办事项"},
								{Key: "#PROMPT#", Value: "Remilia Bot 提醒"},
								{Key: "#LIST#", Obj: []qq.ArkObj{
									{KV: []qq.ArkKVField{{Key: "desc", Value: "需求评审会议 14:00"}}},
									{KV: []qq.ArkKVField{{Key: "desc", Value: "修复 UI 样式问题"}}},
									{KV: []qq.ArkKVField{
										{Key: "desc", Value: "查看详情"},
										{Key: "link", Value: "https://qq.com"},
									}},
									{KV: []qq.ArkKVField{
										{Key: "desc", Value: "提交代码审查"},
										{Key: "link", Value: "https://qq.com"},
									}},
								}},
							}},
						})

					case "24":
						msg = qq.ApplyExtra(platform.TextMessage(""), qq.MessageExtra{
							Ark: &qq.Ark{TemplateID: 24, KV: []qq.ArkKV{
								{Key: "#DESC#", Value: "这是一条图文消息的描述内容"},
								{Key: "#PROMPT#", Value: "图文消息通知"},
								{Key: "#TITLE#", Value: "Remilia Bot 测试标题 - 文本+缩略图模板"},
								{Key: "#METADESC#", Value: "这里是详情描述区域，用于测试文本溢出时的展示效果"},
								{Key: "#IMG#", Value: "https://pub.idqqimg.com/pc/misc/files/20190820/2f4e70ae3355ece23d161cf5334d4fc1jzjfmtep.png"},
								{Key: "#LINK#", Value: "https://qq.com"},
								{Key: "#SUBTITLE#", Value: "Remilia Bot"},
							}},
						})

					case "37":
						msg = qq.ApplyExtra(platform.TextMessage(""), qq.MessageExtra{
							Ark: &qq.Ark{TemplateID: 37, KV: []qq.ArkKV{
								{Key: "#PROMPT#", Value: "大图通知"},
								{Key: "#METATITLE#", Value: "今日精选"},
								{Key: "#METASUBTITLE#", Value: "每日更新，精彩不断"},
								{Key: "#METACOVER#", Value: "https://vfiles.gtimg.cn/vupload/20211029/bf0ed01635493790634.jpg"},
								{Key: "#METAURL#", Value: "https://qq.com"},
							}},
						})

					default:
						return replyCtx(c, "用法: /arktest <23|24|37>\n23=链接+文本列表, 24=文本+缩略图, 37=大图")
					}
					c.Reply(msg)
					/*
						测试了一下返回的都是空结果，result的解析里连错误都没返回，可能这个已经不支持了？
						等待进一步的调查
					*/
					return replyCtx(c, fmt.Sprintf("ARK 模板 %s 已发送", tpl))
				})

			// /multireply — 同消息多次回复演示（msg_seq 递增）
			reg.RegisterCommand("", "/multireply").
				SetDefinition(&command.Definition{Name: "multireply", Description: "回复3次演示msg_seq递增", Category: "demo"}).
				Handle(func(c *eventctx.Context) error {
					for i := range 8 {
						c.Reply(platform.TextMessage(fmt.Sprintf("第 %d 次回复 (msg_seq=%d)", i, i)))
					}
					return nil
				})

			return nil, nil
		},
	}
}
