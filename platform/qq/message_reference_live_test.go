//go:build network

// 真机验证：QQ 群聊/单聊发送带 message_reference（引用气泡）的消息。
//
// 背景：
//   - 官方文档：群消息 / 单聊消息 POST 请求体均可携带 message_reference
//     （引用回复，客户端展示被引用消息气泡），其 message_id 取 REFIDX_...。
//     REFIDX 获取途径：非机器人发的消息来自事件 message_scene.ext 的
//     msg_idx/ref_msg_idx；机器人自己发的消息来自发送响应 ext_info.ref_idx。
//   - 外部开发者反馈：带 message_reference 时无法发送媒体消息。本测试直接
//     构造带 message_reference 的各类载荷（纯文本 / Markdown / 富媒体 /
//     富媒体+文字），在同一会话内真机发送，确认平台是否接受、QQ 端是否
//     正常渲染，用于判断生产代码是否需要做引用兼容。
//
// 引用目标设计：
//   - 默认先发送一条"种子消息"（机器人主动消息），从其发送响应
//     ext_info.ref_idx（缺失时兜底 id）取 REFIDX，作为后续各条消息的
//     message_reference.message_id——全程无需用户在 QQ 端触发。
//   - 若要引用某条用户消息（或人工获取的 REFIDX），设置环境变量
//     QQ_TEST_REF_ID="REFIDX_xxxxxx" 后重跑，将跳过种子消息直接使用该值。
//   - 需要补测某个/某几个用例（避免整批重复骚扰目标）时，设置
//     QQ_TEST_CASES="C,D"（逗号分隔的用例字母），仅发送选中用例。
//
// 运行（PowerShell，仓库根目录；凭据解析与目标解析同
// media_image_text_live_test.go，见 liveCredentials / resolveLiveTargets）：
//
//	$env:QQ_TEST_GROUP_ID="<群 openid>"
//	$env:QQ_TEST_USER_ID="<单聊 openid>"
//	# 可选：$env:QQ_TEST_REF_ID="REFIDX_xxxxxx"（引用指定消息）
//	# 可选：$env:QQ_TEST_CASES="C,D"（只补测 C/D 两个用例）
//	go test -tags network -run TestQQMessageReferenceLive -v ./platform/qq/
package qq

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestQQMessageReferenceLive 真机验证带 message_reference 的各类消息（含媒体）。
func TestQQMessageReferenceLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：go test -tags network -run TestQQMessageReferenceLive -v ./platform/qq/")
	}

	groupID, userID := resolveLiveTargets(t)

	mgr := token.NewManager(&dto.BotInfo{AppID: appID, AppSecret: appSecret})
	t.Cleanup(mgr.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	require.NoError(t, mgr.WaitReadyWithContext(ctx),
		"获取 QQ access token 失败，请检查 QQ_APP_ID / QQ_APP_SECRET 是否有误")

	api := openapi.New(mgr)
	imgData := makeLiveTestPNG(t)

	if groupID != "" {
		t.Run("群聊", func(t *testing.T) {
			runMessageReferenceScenario(t, api, groupID, true, imgData)
		})
	}
	if userID != "" {
		t.Run("单聊", func(t *testing.T) {
			runMessageReferenceScenario(t, api, userID, false, imgData)
		})
	}
}

// messageRefCase 单个待发用例：label 用于日志与 QQ_TEST_CASES 过滤。
type messageRefCase struct {
	label string
	build func() *dto.Message
}

// runMessageReferenceScenario 在单个会话（群聊或单聊）中：先取得一个引用 ID
// （种子消息响应或 QQ_TEST_REF_ID），再发送带 message_reference 的文本 /
// Markdown / 富媒体 / 富媒体+文字，以及不带引用的对照消息。
// QQ_TEST_CASES 非空时只发送选中字母的用例。
func runMessageReferenceScenario(t *testing.T, api openapi.OpenAPI, target string, isGroup bool, imgData []byte) {
	t.Helper()
	scene := "群聊"
	if !isGroup {
		scene = "单聊"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	fileInfo := liveUploadMedia(ctx, t, api, target, isGroup, imgData)
	stamp := time.Now().Format("15:04:05.000")
	text := func(caseID, body string) string {
		return fmt.Sprintf("[%s-%s] %s %s", scene, caseID, stamp, body)
	}

	refID, refSource := resolveLiveReferenceID(ctx, t, api, target, isGroup, scene, stamp)
	t.Logf("%s 本次 message_reference.message_id=%q（来源：%s）", scene, refID, refSource)
	ref := func() *dto.MessageReference {
		return &dto.MessageReference{MessageID: refID, IgnoreGetMessageError: true}
	}

	noTextContent := ""
	if isGroup {
		// 群聊接口 content 被文档标注为必填，与生产 sendMediaMessage 一致补空格兜底
		noTextContent = " "
	}

	cases := []messageRefCase{
		{label: "A-文本+引用", build: func() *dto.Message {
			return &dto.Message{
				Type:             dto.TextMessage,
				Content:          text("A", "msg_type=0 纯文本携带 message_reference。若本条上方出现被引用消息气泡，说明文本引用可用。"),
				MessageReference: ref(),
			}
		}},
		{label: "B-Markdown+引用", build: func() *dto.Message {
			return &dto.Message{
				Type:             dto.MarkdownMessage,
				Markdown:         &dto.Markdown{Content: text("B", "**msg_type=2 Markdown** 携带 message_reference，验证富文本引用是否可用。")},
				MessageReference: ref(),
			}
		}},
		// C：msg_type=7 纯媒体（无正文）+ message_reference —— 验证开发者反馈的疑点
		{label: "C-纯媒体+引用", build: func() *dto.Message {
			return &dto.Message{
				Type:             dto.MediaMessage,
				Media:            &dto.MediaResponse{FileInfo: fileInfo},
				Content:          noTextContent,
				MessageReference: ref(),
			}
		}},
		// D：msg_type=7 媒体 + 多行文字 + message_reference（图文混排带引用）
		{label: "D-图文+引用", build: func() *dto.Message {
			return &dto.Message{
				Type:             dto.MediaMessage,
				Media:            &dto.MediaResponse{FileInfo: fileInfo},
				Content:          text("D", "msg_type=7 图片+文字，同时携带 message_reference。\n第二行：这是多段文字的第二段，用于确认正文没有被吞。"),
				MessageReference: ref(),
			}
		}},
		// E：对照组 msg_type=7 媒体+文字、不带 message_reference（与 D 唯一差异是引用）
		{label: "E-图文无引用对照", build: func() *dto.Message {
			return &dto.Message{
				Type:    dto.MediaMessage,
				Media:   &dto.MediaResponse{FileInfo: fileInfo},
				Content: text("E", "对照组：msg_type=7 图片+文字，不带 message_reference，应与 D 渲染一致。"),
			}
		}},
		// F：对照组 msg_type=0 纯文本、不带 message_reference
		{label: "F-文本无引用对照", build: func() *dto.Message {
			return &dto.Message{
				Type:    dto.TextMessage,
				Content: text("F", "对照组：msg_type=0 纯文本，不带 message_reference。"),
			}
		}},
	}

	var summary []string
	selected := selectedLiveCases()
	for _, c := range cases {
		if selected != nil && !selected[c.label[:1]] {
			continue
		}
		msg := c.build()
		payload, _ := json.Marshal(msg)
		t.Logf("%s 用例 %s payload=%s", scene, c.label, payload)

		var (
			resp gjson.Result
			err  error
		)
		if isGroup {
			resp, err = api.GroupChat(ctx, target, msg)
		} else {
			resp, err = api.SingleChat(ctx, target, msg)
		}
		if err != nil {
			t.Logf("%s 用例 %s 发送失败：%v", scene, c.label, err)
			summary = append(summary, fmt.Sprintf("%s:失败(%v)", c.label, err))
			continue
		}
		t.Logf("%s 用例 %s 发送成功 id=%s，原始响应=%s", scene, c.label, resp.Get("id").String(), resp.Raw)
		summary = append(summary, fmt.Sprintf("%s:成功", c.label))
	}

	// G/H：走生产 Sender 的 QuoteTrigger 路径（主动消息），验证改造后的
	// "消息级按条选引用"开关：G 只标 WithQuoteTrigger，message_reference 由
	// Sender 依据 chat.Tokens[TokenQuoteID] 自动解析；H 为不带标记的对照。
	sender := NewSender(api)
	for _, pc := range []struct {
		letter string
		label  string
		quote  bool
	}{
		{letter: "G", label: "G-主动文本+QuoteTrigger自动引用", quote: true},
		{letter: "H", label: "H-主动文本无引用对照", quote: false},
	} {
		if selected != nil && !selected[pc.letter] {
			continue
		}
		body := text(pc.letter, pc.label+"：G 应显示种子消息的引用气泡；H 不应有气泡。")
		out := platform.TextMessage(body)
		if pc.quote {
			out = out.WithQuoteTrigger()
		}
		chat := platform.ChatInfo{
			ID:      target,
			IsGroup: isGroup,
			Tokens:  map[string]string{TokenQuoteID: refID},
		}
		res, err := sender.Send(ctx, platform.SendRequest{Target: chat, Message: out})
		if err != nil {
			t.Logf("%s 用例 %s 发送失败：%v", scene, pc.label, err)
			summary = append(summary, fmt.Sprintf("%s:失败(%v)", pc.label, err))
			continue
		}
		t.Logf("%s 用例 %s 发送成功 id=%s（Sender 路径，TokenQuoteID=%q）",
			scene, pc.label, res.MessageID, refID)
		summary = append(summary, fmt.Sprintf("%s:成功", pc.label))
	}

	t.Logf("%s 发送汇总：%s", scene, strings.Join(summary, "；"))
	t.Logf("提示：请到 QQ 端查看 %s 中已发送用例：带引用的用例应显示引用气泡；C/D 若图片不显示或被平台拒绝，即对应开发者反馈的\"带 message_reference 不能发媒体\"问题；E/F 为对照组；G 应显示引用气泡、H 不应有气泡。", scene)
}

// selectedLiveCases 解析 QQ_TEST_CASES（如 "C,D"）为用例字母集合；
// 未设置时返回 nil 表示全量发送。
func selectedLiveCases() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("QQ_TEST_CASES"))
	if raw == "" {
		return nil
	}
	sel := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		if p := strings.ToUpper(strings.TrimSpace(part)); len(p) == 1 {
			sel[p] = true
		}
	}
	return sel
}

// resolveLiveReferenceID 确定本次测试的 message_reference.message_id：
// 优先取环境变量 QQ_TEST_REF_ID（可指向某条用户消息的 REFIDX）；
// 未设置时先发一条种子消息，从其发送响应 ext_info.ref_idx（缺失时兜底 id）提取。
func resolveLiveReferenceID(ctx context.Context, t *testing.T, api openapi.OpenAPI, target string, isGroup bool, scene, stamp string) (refID, source string) {
	t.Helper()
	if refID = strings.TrimSpace(os.Getenv("QQ_TEST_REF_ID")); refID != "" {
		return refID, "环境变量 QQ_TEST_REF_ID（可引用用户消息，REFIDX 来自事件或人工获取）"
	}

	resp := liveSendSeed(ctx, t, api, target, isGroup, "种子-被引用消息", &dto.Message{
		Type:    dto.TextMessage,
		Content: fmt.Sprintf("[%s-种子] %s 我是被引用的种子消息：后续带 message_reference 的测试消息应在本条上方显示引用气泡。", scene, stamp),
	})
	refID = resp.Get("ext_info.ref_idx").String()
	source = "种子消息发送响应 ext_info.ref_idx"
	if refID == "" {
		refID = resp.Get("id").String()
		source = "种子消息发送响应 id（ext_info.ref_idx 缺失时兜底）"
	}
	if refID == "" {
		t.Fatalf("%s 未能从种子消息响应取得引用 ID（ext_info.ref_idx / id 均为空）。可把要引用的消息对应的 REFIDX 通过环境变量 QQ_TEST_REF_ID 传入后重跑。", scene)
	}
	return refID, source
}

// liveSendSeed 发送种子消息并返回完整响应（供提取 ext_info.ref_idx）。
func liveSendSeed(ctx context.Context, t *testing.T, api openapi.OpenAPI, target string, isGroup bool, label string, msg *dto.Message) gjson.Result {
	t.Helper()
	payload, _ := json.Marshal(msg)
	scene := "群聊"
	if !isGroup {
		scene = "单聊"
	}

	var (
		resp gjson.Result
		err  error
	)
	if isGroup {
		resp, err = api.GroupChat(ctx, target, msg)
	} else {
		resp, err = api.SingleChat(ctx, target, msg)
	}
	t.Logf("%s 用例 %s payload=%s", scene, label, payload)
	if err != nil {
		t.Fatalf("%s 用例 %s 发送失败：%v", scene, label, err)
	}
	t.Logf("%s 用例 %s 发送成功 id=%s，原始响应=%s", scene, label, resp.Get("id").String(), resp.Raw)
	return resp
}
