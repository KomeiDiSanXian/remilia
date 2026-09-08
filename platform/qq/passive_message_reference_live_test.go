//go:build network

// 真机验证：被动回复（携带 msg_id 被动授权）下，message_reference（引用气泡）
// 能否与各类载荷共存（文本 / Markdown / 纯媒体 / 图文混排）。
//
// 背景：
//   - message_reference_live_test.go 已真机验证"主动消息 + message_reference"
//     全部正常（含 msg_type=7 媒体）。
//   - 本测试验证生产改造方向的前置问题："被动回复 + message_reference"是否也可
//     用——即带 msg_id 授权的回复同时携带引用气泡时，文本/媒体/图文是否正常。
//
// 测试流程（每个目标会话一轮"触发"）：
//   - 测试先主动发一条"种子消息"并取 ext_info.ref_idx（引用 ID 兜底来源，
//     也可用于验证被动回复引用机器人自己的消息）。
//   - 随后测试打印一个唯一触发词 TOKEN，等待人工发送触发消息：
//     单聊：私聊机器人发送正文含 TOKEN 的消息；
//     群聊：在测试群里 @机器人 并发送正文含 TOKEN 的消息（GROUP_AT_MESSAGE_CREATE）。
//   - 命中后，取事件 d.id 作为 msg_id（被动授权），并优先从 message_scene.ext 取
//     msg_idx（触发消息自身 REFIDX）作为引用目标；取不到时回退 ref_msg_idx，
//     再回退种子 ref_idx。随后以递增 msg_seq 发送 A~F 六条被动回复。
//
// 可选第二轮（引用触发，验证 ref_msg_idx 场景，即用户"引用/回复"某条消息后由
// 机器人引用该被引用消息）：设置 $env:QQ_TEST_PASSIVE_QUOTE='1'。第二轮要求
// 人工发送的消息必须是对某条历史消息的"引用（回复）"且正文含第二轮 TOKEN。
//
// 运行（PowerShell，从仓库根目录执行；凭据解析同其他 live 测试，见
// media_image_text_live_test.go 的 liveCredentials / resolveLiveTargets）：
//
//	$env:QQ_TEST_GROUP_ID="<群 openid>"   # 可选（群聊场景）
//	$env:QQ_TEST_USER_ID="<单聊 openid>"   # 可选（单聊场景）
//	# 可选：$env:QQ_TEST_PASSIVE_QUOTE='1'   # 加跑"引用触发"第二轮
//	# 可选：$env:QQ_TEST_PASSIVE_WAIT='180'  # 每轮等待触发消息的秒数
//	# 可选：$env:QQ_TEST_WEBHOOK_ADDR=':9000' # Webhook 监听地址，默认取 config.yaml
//	go test -tags network -run TestQQPassiveMessageReferenceLive -v -timeout 15m ./platform/qq/
//
// 事件接收方式：与线上 bot 一致走 QQ Webhook（无需在 QQ 开放平台把回调从
// Webhook 切换成 WebSocket）。测试在本地启动 WebhookService 监听
// bot.qq.webhook.host/port（默认 0.0.0.0:9000，即 QQ 控制台回调 URL 指向的
// 内网端口），QQ 推送的 C2C_MESSAGE_CREATE / GROUP_AT_MESSAGE_CREATE 事件
// 会原样进入测试的事件流。
//
// 注意：运行前请先停掉线上 bot 进程——同一监听地址无法二次绑定，且同一 AppID
// 事件不应有两个接收端。测试结束恢复启动线上 bot 即可，QQ 控制台无需改动。
package qq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v3"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestQQPassiveMessageReferenceLive 真机验证被动回复携带 message_reference。
func TestQQPassiveMessageReferenceLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：go test -tags network -run TestQQPassiveMessageReferenceLive -v -timeout 15m ./platform/qq/")
	}

	groupID, userID := resolveLiveTargets(t)

	mgr := token.NewManager(&dto.BotInfo{AppID: appID, AppSecret: appSecret})
	t.Cleanup(mgr.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	require.NoError(t, mgr.WaitReadyWithContext(ctx),
		"获取 QQ access token 失败，请检查 QQ_APP_ID / QQ_APP_SECRET 是否有误")

	api := openapi.New(mgr)

	// 通过 QQ Webhook 接收被动事件（与线上 bot 一致，无需在 QQ 控制台切换协议）。
	// 监听地址默认取 config.yaml bot.qq.webhook，与线上 bot 相同；因此测试期间
	// 必须先停掉线上 bot，避免端口占用与事件双接收。
	svc := NewWebhookServiceWithConfig(webhookListenAddr(t), &dto.BotInfo{
		AppID: appID, AppSecret: appSecret,
	}, config.WebhookConfig{})
	svc.WithAPI(api)
	require.NoError(t, svc.start(ctx),
		"启动 QQ Webhook 事件服务失败：请确认已停掉线上 bot（端口占用）且 QQ 控制台回调 URL 能转发到该监听地址")
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = svc.stop(stopCtx)
	})
	stream := svc.EventStream()
	if stream == nil {
		t.Fatal("QQ Webhook EventStream 为空（start 后应立即可用）")
	}

	imgData := makeLiveTestPNG(t)

	if groupID != "" {
		t.Run("群聊被动引用", func(t *testing.T) {
			runPassiveReferenceScene(t, ctx, stream, api, svc, groupID, true, imgData)
		})
	}
	if userID != "" {
		t.Run("单聊被动引用", func(t *testing.T) {
			runPassiveReferenceScene(t, ctx, stream, api, svc, userID, false, imgData)
		})
	}
}

// passiveRound 描述一轮需要人工触发的被动验证。
type passiveRound struct {
	id        string // R1 / R2 / AUTO，用于区分触发词与日志
	needQuote bool   // 要求触发消息是 message_type=103 的引用消息
	autoQuote bool   // 走生产 Sender 的 WithQuoteTrigger 路径（验证自动引用）
	refPick   string // 引用目标优先取 "msg_idx" 还是 "ref_msg_idx"
	desc      string
}

// passiveTrigger 从命中的事件里提取被动回复所需的关键字段。
type passiveTrigger struct {
	msgID     string // 事件 d.id，作为 msg_id 被动授权
	msgType   int64  // message_type（0/103 等）
	content   string
	msgIdx    string // message_scene.ext 中 msg_idx=REFIDX_xxx（触发消息自身）
	refMsgIdx string // message_scene.ext 中 ref_msg_idx=REFIDX_xxx（被引用消息）
	detail    string // 原始 d 详情（日志用）
}

// runPassiveReferenceScene 在单个会话（群聊或单聊）中完成一轮或多轮被动验证。
func runPassiveReferenceScene(t *testing.T, ctx context.Context, stream <-chan *dto.Payload, api openapi.OpenAPI, svc *WebhookService, target string, isGroup bool, imgData []byte) {
	t.Helper()
	scene := "单聊"
	sceneShort := "C2C"
	if isGroup {
		scene = "群聊"
		sceneShort = "GRP"
	}

	waitSecs := 180
	if raw := os.Getenv("QQ_TEST_PASSIVE_WAIT"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			waitSecs = v
		}
	}
	roundWait := time.Duration(waitSecs) * time.Second

	fileInfo := liveUploadMedia(ctx, t, api, target, isGroup, imgData)

	// 种子消息：提供 ref_idx 兜底，并验证被动回复可引用机器人自己的消息。
	stamp := time.Now().Format("150405.000")
	seedRef, seedSource := resolveLiveReferenceID(ctx, t, api, target, isGroup, scene, stamp)
	t.Logf("%s 种子 REFIDX=%q（来源：%s）", scene, seedRef, seedSource)

	rounds := []passiveRound{
		{
			id:        "R1",
			needQuote: false,
			refPick:   "msg_idx",
			desc:      "普通触发：引用目标取触发消息自身 msg_idx（无则 ref_msg_idx，再兜底种子）",
		},
	}
	if isTruthy(os.Getenv("QQ_TEST_PASSIVE_QUOTE")) {
		rounds = append(rounds, passiveRound{
			id:        "R2",
			needQuote: true,
			refPick:   "ref_msg_idx",
			desc:      "引用触发（103）：引用目标取触发消息 ref_msg_idx（被引用消息）",
		})
	}
	if isTruthy(os.Getenv("QQ_TEST_AUTO_QUOTE")) {
		rounds = append(rounds, passiveRound{
			id:        "AUTO",
			autoQuote: true,
			refPick:   "msg_idx",
			desc:      "自动引用（QuoteTrigger）：回复经生产 Sender 发送，不手动携带 message_reference",
		})
	}

	for i, round := range rounds {
		roundToken := fmt.Sprintf("PASREF-%s-%s-%s", sceneShort, stamp, round.id)
		t.Logf("")
		t.Logf("============== 请操作：%s 第 %d/%d 轮（%s） ==============", scene, i+1, len(rounds), round.desc)
		if isGroup {
			t.Logf("请在测试群里 @机器人 发送一条消息，正文包含触发词：%s", roundToken)
			t.Logf("示例：@机器人 %s 请回复", roundToken)
			if round.needQuote {
				t.Logf("注意：本条必须是『引用某条历史消息』后发送（长按消息→回复），被引用消息内容不限。")
			}
		} else {
			t.Logf("请私聊机器人发送一条消息，正文包含触发词：%s", roundToken)
			t.Logf("示例：%s 请回复", roundToken)
			if round.needQuote {
				t.Logf("注意：本条必须是『引用某条历史消息』后发送（长按消息→回复）。")
			}
		}
		t.Logf("等待命中（最长 %v）……", roundWait)
		t.Logf("============================================================")

		tr := waitPassiveTrigger(t, ctx, stream, scene, target, isGroup, roundToken, round.needQuote, roundWait)
		if tr.msgID == "" {
			t.Fatalf("%s 触发消息缺少 id，无法作为 msg_id 被动授权", scene)
		}
		refMain, refSource := pickPassiveRef(tr, round, seedRef, seedSource)
		t.Logf("%s 本轮引用目标 message_reference.message_id=%q（来源：%s）", scene, refMain, refSource)

		if round.autoQuote {
			runAutoQuoteSenderCase(t, ctx, svc, scene, target, isGroup, round.id, tr)
		} else {
			runPassiveReplyCases(t, ctx, api, scene, target, isGroup, round.id, tr.msgID, refMain, seedRef, fileInfo)
		}
	}
}

// pickPassiveRef 按轮次语义选择引用目标：
// ref_msg_idx（被引用消息）> msg_idx（触发消息自身）> 种子消息 ref_idx。
func pickPassiveRef(tr passiveTrigger, round passiveRound, seedRef, seedSource string) (ref, source string) {
	if round.refPick == "ref_msg_idx" && tr.refMsgIdx != "" {
		return tr.refMsgIdx, "触发消息 ref_msg_idx（被引用消息）"
	}
	if tr.msgIdx != "" {
		return tr.msgIdx, "触发消息 msg_idx（触发消息自身）"
	}
	if tr.refMsgIdx != "" {
		return tr.refMsgIdx, "触发消息 ref_msg_idx（无 msg_idx 时的回退）"
	}
	return seedRef, fmt.Sprintf("种子消息（触发消息无可用 REFIDX 时的兜底，来源：%s）", seedSource)
}

// runPassiveReplyCases 用同一 msg_id（被动授权）发送 A~F 各载荷。
func runPassiveReplyCases(t *testing.T, ctx context.Context, api openapi.OpenAPI, scene, target string, isGroup bool, roundID, msgID, refMain, seedRef, fileInfo string) {
	t.Helper()
	ref := func() *dto.MessageReference {
		return &dto.MessageReference{MessageID: refMain, IgnoreGetMessageError: true}
	}
	seedRefMsg := func() *dto.MessageReference {
		return &dto.MessageReference{MessageID: seedRef, IgnoreGetMessageError: true}
	}
	stamp := time.Now().Format("15:04:05.000")
	label := func(caseID, body string) string {
		return fmt.Sprintf("[%s-%s-%s %s] %s", scene, roundID, caseID, stamp, body)
	}
	// 群聊接口 content 被文档标注为必填：纯图片无正文时补空格兜底。
	noTextContent := ""
	if isGroup {
		noTextContent = " "
	}

	var summary []string
	seq := uint64(0)
	send := func(caseID string, m *dto.Message) {
		t.Helper()
		seq++
		m.MessageID = dto.EventID(msgID)
		m.MessageSeq = seq
		payload, _ := json.Marshal(m)

		var (
			resp gjson.Result
			err  error
		)
		if isGroup {
			resp, err = api.GroupChat(ctx, target, m)
		} else {
			resp, err = api.SingleChat(ctx, target, m)
		}
		t.Logf("%s 用例 %s payload=%s", scene, caseID, payload)
		if err != nil {
			t.Logf("%s 用例 %s 发送失败：%v", scene, caseID, err)
			summary = append(summary, caseID+":失败("+err.Error()+")")
			time.Sleep(time.Second)
			return
		}
		t.Logf("%s 用例 %s 发送成功 id=%s，原始响应=%s", scene, caseID, resp.Get("id").String(), resp.Raw)
		summary = append(summary, caseID+":成功")
		time.Sleep(time.Second)
	}

	send("A-文本+引用", &dto.Message{
		Type:             dto.TextMessage,
		Content:          label("A", "被动文本回复+引用气泡：本条应显示上方被引用消息的气泡，且文字完整。"),
		MessageReference: ref(),
	})
	send("B-Markdown+引用", &dto.Message{
		Type:             dto.MarkdownMessage,
		Markdown:         &dto.Markdown{Content: label("B", "被动 **Markdown** 回复+引用气泡：本条应显示加粗文字与引用气泡。")},
		MessageReference: ref(),
	})
	send("C-纯媒体+引用", &dto.Message{
		Type:             dto.MediaMessage,
		Media:            &dto.MediaResponse{FileInfo: fileInfo},
		Content:          noTextContent,
		MessageReference: ref(),
	})
	send("D-图文+引用", &dto.Message{
		Type:             dto.MediaMessage,
		Media:            &dto.MediaResponse{FileInfo: fileInfo},
		Content:          label("D", "被动图文回复+引用气泡，第一行。\n第二行：多段文字应完整显示，不被吞掉。"),
		MessageReference: ref(),
	})
	send("E-文本无引用对照", &dto.Message{
		Type:    dto.TextMessage,
		Content: label("E", "被动文本回复，不带 message_reference（对照组，验证 msg_id 授权本身正常）。"),
	})
	if seedRef != "" && seedRef != refMain {
		send("F-图文+引用种子", &dto.Message{
			Type:             dto.MediaMessage,
			Media:            &dto.MediaResponse{FileInfo: fileInfo},
			Content:          label("F", "被动图文回复，引用机器人自己的种子消息（验证 ext_info.ref_idx 来源）。"),
			MessageReference: seedRefMsg(),
		})
	}

	t.Logf("%s 第%s轮发送汇总：%s", scene, roundID, strings.Join(summary, "；"))
	t.Logf("提示：请到 QQ 端查看 %s 第%s轮的 A~F：A 文本+引用气泡；B Markdown+引用气泡；C 纯图片+引用气泡；D 图片+两行文字+引用气泡；E 纯文本对照（无气泡）；F（若发送）图片+文字+引用种子消息。", scene, roundID)
}

// runAutoQuoteSenderCase 走生产 Sender 路径验证 QuoteTrigger：消息只标记
// WithQuoteTrigger，msg_id 被动授权与引用目标 REFIDX（TokenQuoteID）由
// ChatInfo.Tokens 解析（与线上事件解析写入一致），验证 Sender 自动挂
// message_reference 后真机展示引用气泡。
func runAutoQuoteSenderCase(t *testing.T, ctx context.Context, svc *WebhookService, scene, target string, isGroup bool, roundID string, tr passiveTrigger) {
	t.Helper()
	stamp := time.Now().Format("15:04:05.000")
	body := fmt.Sprintf("[%s-%s-AUTO %s] WithQuoteTrigger 自动引用：本条应显示触发消息的引用气泡且文字完整。",
		scene, roundID, stamp)
	chat := platform.ChatInfo{
		ID:      target,
		IsGroup: isGroup,
		Tokens:  map[string]string{TokenMsgID: tr.msgID},
	}
	if tr.msgIdx != "" {
		chat.Tokens[TokenQuoteID] = tr.msgIdx
	}
	t.Logf("%s %s轮 自动引用：Sender 构造（msg_id=%s，TokenQuoteID=%q，QuoteTrigger=true）",
		scene, roundID, tr.msgID, chat.Tokens[TokenQuoteID])

	sender := svc.Sender()
	if sender == nil {
		t.Fatalf("%s %s轮 自动引用：WebhookService 未持有 sender", scene, roundID)
	}
	res, err := sender.Send(ctx, platform.SendRequest{Target: chat, Message: platform.TextMessage(body).WithQuoteTrigger()})
	if err != nil {
		t.Logf("%s %s轮 自动引用 发送失败：%v", scene, roundID, err)
		t.FailNow()
		return
	}
	t.Logf("%s %s轮 自动引用 发送成功 id=%s", scene, roundID, res.MessageID)
	time.Sleep(time.Second)
	t.Logf("提示：请到 QQ 端查看 %s %s轮 自动引用消息：应显示触发消息的引用气泡（无气泡则 QuoteTrigger 未生效）。",
		scene, roundID)
}

// waitPassiveTrigger 监听事件流，等待正文含 tok 的触发消息。
func waitPassiveTrigger(t *testing.T, ctx context.Context, stream <-chan *dto.Payload, scene, target string, isGroup bool, tok string, needQuote bool, wait time.Duration) passiveTrigger {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("%s 整体上下文结束，仍未等到正文含 %q 的触发消息：%v", scene, tok, ctx.Err())
		case <-timer.C:
			kind := "（群聊需 @机器人）"
			if !isGroup {
				kind = "（单聊私发）"
			}
			t.Fatalf("%s 等待触发超时（%v）：未收到正文含 %q 的消息%s。请重跑测试并按提示发送。", scene, wait, tok, kind)
		case p := <-stream:
			if p == nil {
				t.Fatalf("%s 事件流已关闭，无法等待触发消息", scene)
			}
			tr, matched := matchPassiveTrigger(p, target, isGroup, tok, needQuote)
			dto.ReleasePayload(p)
			if !matched {
				continue
			}
			t.Logf("%s 命中触发：msg_id=%q message_type=%d msg_idx=%q ref_msg_idx=%q",
				scene, tr.msgID, tr.msgType, tr.msgIdx, tr.refMsgIdx)
			t.Logf("%s 触发消息原始 detail=%s", scene, tr.detail)
			return tr
		}
	}
}

// matchPassiveTrigger 判断 payload 是否命中目标会话中的触发消息。
// 命中后从 d 提取被动回复所需字段（字符串均已拷贝，payload 可安全归还对象池）。
func matchPassiveTrigger(p *dto.Payload, target string, isGroup bool, tok string, needQuote bool) (passiveTrigger, bool) {
	var tr passiveTrigger
	chatID := ""
	if isGroup {
		switch p.Type {
		case dto.GroupAtMessageCreate, dto.GroupMessageCreate:
		default:
			return tr, false
		}
		chatID = gjson.GetBytes(p.Detail, "group_openid").String()
	} else {
		if p.Type != dto.C2CMessageCreate {
			return tr, false
		}
		chatID = gjson.GetBytes(p.Detail, "author.user_openid").String()
	}
	if chatID != target {
		return tr, false
	}
	tr.content = gjson.GetBytes(p.Detail, "content").String()
	if !strings.Contains(tr.content, tok) {
		return tr, false
	}
	tr.msgType = gjson.GetBytes(p.Detail, "message_type").Int()
	ext := gjson.GetBytes(p.Detail, "message_scene.ext")
	tr.msgIdx = extPrefixValue(ext, "msg_idx=")
	tr.refMsgIdx = extPrefixValue(ext, "ref_msg_idx=")
	if needQuote && (tr.msgType != 103 || tr.refMsgIdx == "") {
		return tr, false
	}
	tr.msgID = gjson.GetBytes(p.Detail, "id").String()
	tr.detail = string(p.Detail)
	return tr, true
}

// extPrefixValue 从 message_scene.ext 数组项中提取 "prefix=value" 的值。
// 实测（2026-08 报文核验）：ext 是字符串数组，形如 "msg_idx=REFIDX_..." /
// "ref_msg_idx=REFIDX_..."；103 引用消息两者可能同时出现。
func extPrefixValue(ext gjson.Result, prefix string) string {
	if !ext.IsArray() {
		return ""
	}
	for _, item := range ext.Array() {
		if s := item.String(); strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return ""
}

// webhookListenAddr 返回本测试用于接收 QQ 回调事件的本地监听地址：
// 环境变量 QQ_TEST_WEBHOOK_ADDR（如 ":9000"）优先；未设置时读取仓库根目录
// config.yaml 的 bot.qq.webhook.host/port——与线上 bot 相同的监听地址，
// 保证 QQ 控制台已配置的回调 URL 能直接把事件转发到本测试。
func webhookListenAddr(t *testing.T) string {
	t.Helper()
	if raw := strings.TrimSpace(os.Getenv("QQ_TEST_WEBHOOK_ADDR")); raw != "" {
		return raw
	}
	for _, path := range []string{"../../config.yaml", "config.yaml"} {
		var doc struct {
			Bot struct {
				QQ struct {
					Webhook struct {
						Host string `yaml:"host"`
						Port int    `yaml:"port"`
					} `yaml:"webhook"`
				} `yaml:"qq"`
			} `yaml:"bot"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			continue
		}
		wh := doc.Bot.QQ.Webhook
		if wh.Port > 0 {
			host := wh.Host
			if host == "" {
				host = "0.0.0.0"
			}
			t.Logf("Webhook 监听地址取自 %s 的 bot.qq.webhook（可用 QQ_TEST_WEBHOOK_ADDR 覆盖）：%s:%d",
				path, host, wh.Port)
			return fmt.Sprintf("%s:%d", host, wh.Port)
		}
	}
	t.Fatalf("无法确定 Webhook 监听地址：请设置 QQ_TEST_WEBHOOK_ADDR（如 \":9000\"）或在 config.yaml 配置 bot.qq.webhook.host/port")
	return ""
}
