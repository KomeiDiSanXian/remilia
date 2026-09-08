//go:build network

// 真机探针（未跟踪，不入库）：确认 QQ 群聊纯图片消息"图下方多余空格"是否来自
// sender.go sendMediaMessage 的群聊 content:" " 兜底（7716e80 引入，当时官方文档
// 把群聊 content 标注为必填）。2026-08-11 版文档已把群聊 content 标为可选，
// 富媒体消息官方示例本身就不带 content：
//
//	{"msg_type":7,"media":{"file_info":"..."}}
//
// 本探针在同一个群里发送（每张图主色调不同，肉眼可直接对照）：
//
//	S. 种子文本消息（仅当需要跑 C 时发送），取其响应 ext_info.ref_idx 作引用 ID
//	A. 生产 qqSender.Send 纯图路径（现状：群聊会补 content:" "；修复后应省略 content）
//	B. 直接接口 msg_type=7 + media，不带 content（官方示例形态，对照组）
//	C. 直接接口 msg_type=7 + media + message_reference，不带 content
//	   （覆盖"被动纯图回复带引用气泡"在无 content 时是否仍被接受）
//
// 判读：
//   - 若 A 图片下方有额外空格行而 B/C 没有 → 空格确由 content:" " 引起；
//   - 若 B/C 发送成功且 QQ 端正常显示 → 群聊媒体消息可省略 content，占位可移除。
//
// 环境变量（凭据/目标解析复用 media_image_text_live_test.go 的
// liveCredentials / resolveLiveTargets）：
//
//	$env:QQ_TEST_GROUP_ID="<群 openid>"
//	$env:QQ_SPACE_CASES="A,B,C"   # 可选：只发选中用例（逗号分隔），默认 A,B,C
//	go test -tags network -run TestQQGroupImageSpaceProbeLive -v -timeout 15m ./platform/qq/
package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
)

// TestQQGroupImageSpaceProbeLive 真机探针：验证群聊纯图多余空格与 content:" " 的因果，
// 并确认去掉占位（省略 content）后群聊媒体消息仍可发送。
func TestQQGroupImageSpaceProbeLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：go test -tags network -run TestQQGroupImageSpaceProbeLive -v ./platform/qq/")
	}
	groupID, _ := resolveLiveTargets(t)
	if groupID == "" {
		t.Skipf("未配置群聊目标：设置 QQ_TEST_GROUP_ID（本探针仅覆盖群聊空格问题）")
	}

	mgr := token.NewManager(&dto.BotInfo{AppID: appID, AppSecret: appSecret})
	t.Cleanup(mgr.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	require.NoError(t, mgr.WaitReadyWithContext(ctx),
		"获取 QQ access token 失败，请检查 QQ_APP_ID / QQ_APP_SECRET 是否有误")

	spaceProbeRun(t, ctx, openapi.New(mgr), groupID)
}

// spaceProbeRun 在群聊中按需发送种子/S/A/B/C 用例并打印实际载荷。
func spaceProbeRun(t *testing.T, ctx context.Context, api openapi.OpenAPI, groupID string) {
	t.Helper()
	selected := spaceProbeSelected()

	// S 种子：为 C 提供 REFIDX（机器人主动消息响应 ext_info.ref_idx）。
	refID := ""
	if selected["C"] {
		stamp := time.Now().Format("15:04:05.000")
		seedMsg := &dto.Message{
			Type:    dto.TextMessage,
			Content: fmt.Sprintf("[群聊-种子] %s 我是纯图空格探针的种子消息，C 的引用气泡应指向本条。", stamp),
		}
		resp, err := api.GroupChat(ctx, groupID, seedMsg)
		if err != nil {
			t.Logf("群聊 种子发送失败：%v（C 将跳过引用）", err)
		} else {
			refID = resp.Get("ext_info.ref_idx").String()
			t.Logf("群聊 种子发送成功 id=%s，REFIDX=%q（原始响应=%s）", resp.Get("id").String(), refID, resp.Raw)
		}
	}

	var summary []string

	// A：生产 qqSender.Send 纯图路径（现状群聊补 content:" "，修复后应省略）。
	if selected["A"] {
		rec := &captureOpenAPI{OpenAPI: api}
		sender := NewSender(rec)
		sendResult, err := sender.Send(ctx, platform.SendRequest{
			Target: platform.ChatInfo{ID: groupID, IsGroup: true},
			Message: platform.OutboundMessage{Attachments: []platform.Attachment{
				{Kind: platform.AttachmentKindImage, Data: spaceProbePNG(t, color.RGBA{R: 220, G: 45, B: 45, A: 255})},
			}},
		})
		if got := rec.capture(); got != nil {
			payload, _ := json.Marshal(got)
			t.Logf("群聊 用例 A 生产路径实际发出 payload=%s", payload)
		}
		if err != nil {
			t.Logf("群聊 用例 A 生产路径发送失败：%v", err)
			summary = append(summary, "A:失败")
		} else {
			t.Logf("群聊 用例 A 生产路径发送成功 id=%s", sendResult.MessageID)
			summary = append(summary, "A:成功")
		}
	}

	// B：直接接口 msg_type=7 + media，不带 content（官方示例形态）。
	if selected["B"] {
		fileInfo := liveUploadMedia(ctx, t, api, groupID, true, spaceProbePNG(t, color.RGBA{R: 45, G: 200, B: 70, A: 255}))
		liveSendRaw(ctx, t, api, groupID, true, "B-无content官方形态", &dto.Message{
			Type:  dto.MediaMessage,
			Media: &dto.MediaResponse{FileInfo: fileInfo},
		})
	}

	// C：直接接口 msg_type=7 + media + message_reference，不带 content。
	if selected["C"] && refID != "" {
		fileInfo := liveUploadMedia(ctx, t, api, groupID, true, spaceProbePNG(t, color.RGBA{R: 55, G: 90, B: 220, A: 255}))
		liveSendRaw(ctx, t, api, groupID, true, "C-无content+引用", &dto.Message{
			Type:             dto.MediaMessage,
			Media:            &dto.MediaResponse{FileInfo: fileInfo},
			MessageReference: &dto.MessageReference{MessageID: refID, IgnoreGetMessageError: true},
		})
	}

	if len(summary) > 0 {
		t.Logf("用例 A 发送汇总：%s", strings.Join(summary, "；"))
	}
	t.Logf("请到 QQ 端查看本群依次发出的 A(红)/B(绿)/C(蓝)：若 A 图片下方有额外空格行而 B/C 没有，说明空格由 content:\" \" 引起；B/C 能成功发送说明群聊纯图可省略 content。")
}

// spaceProbeSelected 解析 QQ_SPACE_CASES（逗号分隔的用例字母，默认 A,B,C）。
func spaceProbeSelected() map[string]bool {
	selected := map[string]bool{"A": true, "B": true, "C": true}
	raw := os.Getenv("QQ_SPACE_CASES")
	if raw == "" {
		return selected
	}
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part != "" {
			out[part] = true
		}
	}
	return out
}

// spaceProbePNG 生成 320x320 深浅交替的斜条纹测试图，用 base 色区分各用例。
func spaceProbePNG(t *testing.T, base color.RGBA) []byte {
	t.Helper()
	const size = 320
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := base
			if (x+y)/40%2 == 0 {
				c.R = uint8(int(c.R) * 2 / 3)
				c.G = uint8(int(c.G) * 2 / 3)
				c.B = uint8(int(c.B) * 2 / 3)
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}
