//go:build network

// 真机验证 2026-10-21 / 2026-10-28 官方新增的两个能力：
//
//	A. 图文卡片消息（msg_type=8，2026-10-28 起群聊与单聊都支持）
//	B. 素材管理（2026-10-21 新增）：把图片上传到平台换 qqbot-media:// 引用，
//	   再在 Markdown 里引用，不依赖公网图床
//
// 每个场景发几条消息，判读靠 QQ 端直接看：
//
//	A1. 官方示例形态：msg_type=8 + card{type:tuwen}（含图片、标题、描述、链接）
//	A2. sender 路径：platform.Embed → 自动映射成图文卡片（库使用者最常用的入口）
//	A3. 对照组：同一张图 + 同一段文字走 Markdown（确认卡片渲染确实不同于普通图文）
//
//	B1. 上传一张本地生成的 PNG → 素材引用 → Markdown 内嵌
//	B2. 查询素材列表，确认 B1 的图片在列表中且 expires_at 已设置
//	B3. 删除 B1 的素材，确认配额已释放（列表里不再出现）
//
// 运行（PowerShell，在仓库根目录执行；凭据缺省读 config.yaml 的 bot.qq）：
//
//	$env:QQ_TEST_USER_ID="<单聊 openid>"    # 单聊场景
//	$env:QQ_TEST_GROUP_ID="<群 openid>"     # 群聊场景（可选，两个都可配）
//	go test -tags network -run TestQQCardAndMaterialLive -v -timeout 10m ./platform/qq/
//
// 可选：
//
//	$env:QQ_TEST_CARD_IMAGE_URL="https://..."   # 覆盖卡片预览图（必须 https）
//	$env:QQ_TEST_KEEP_MATERIAL="true"           # 保留上传的素材（默认发完即删）
package qq

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
)

// liveCardImageURL 卡片预览图默认值：官方文档示例用的公网 https 图片。
// 卡片要求 pic_url 必须是 https，用 http 或不存在的域名都会被平台拒绝。
const liveCardImageURL = "https://resource5-1255303497.cos.ap-guangzhou.myqcloud.com/abcmouse_word_watch/markdown/building.png"

// TestQQCardAndMaterialLive 真机验证图文卡片与素材管理。
func TestQQCardAndMaterialLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：go test -tags network -run TestQQCardAndMaterialLive -v ./platform/qq/")
	}

	groupID, userID := resolveLiveTargets(t)

	mgr := token.NewManager(&dto.BotInfo{AppID: appID, AppSecret: appSecret})
	t.Cleanup(mgr.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	require.NoError(t, mgr.WaitReadyWithContext(ctx),
		"获取 QQ access token 失败，请检查 QQ_APP_ID / QQ_APP_SECRET 是否有误")

	api := openapi.New(mgr)

	if groupID != "" {
		t.Run("群聊", func(t *testing.T) {
			runCardAndMaterialScenario(t, api, groupID, true)
		})
	}
	if userID != "" {
		t.Run("单聊", func(t *testing.T) {
			runCardAndMaterialScenario(t, api, userID, false)
		})
	}
}

// runCardAndMaterialScenario 在单个场景（群聊或单聊）里跑完卡片与素材两条链路。
func runCardAndMaterialScenario(t *testing.T, api openapi.OpenAPI, target string, isGroup bool) {
	t.Helper()
	scene := "单聊"
	if isGroup {
		scene = "群聊"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	stamp := time.Now().Format("15:04:05.000")
	label := func(id, body string) string {
		return fmt.Sprintf("[%s-%s] %s %s", scene, id, stamp, body)
	}

	picURL := os.Getenv("QQ_TEST_CARD_IMAGE_URL")
	if picURL == "" {
		picURL = liveCardImageURL
	}
	linkURL := "https://bot.q.qq.com/wiki/develop/api-v2/"

	// ── A. 图文卡片（msg_type=8）─────────────────────────────────────────

	// A1：官方示例形态，直接构造 dto.Message，验证平台侧卡片能力本身可用。
	liveSendRaw(ctx, t, api, target, isGroup, "A1-卡片-接口形态", &dto.Message{
		Type: dto.CardMessage,
		Card: dto.NewTuwenCard(
			"Remilia 图文卡片",
			label("A1", "这是 msg_type=8 图文卡片（接口直发）。看到预览图+标题+描述说明卡片能力可用。"),
			linkURL,
			picURL,
		),
	})

	// A2：库使用者路径 —— 写跨平台的 Embed，QQ Sender 自动升级成卡片。
	sender := NewSender(api)
	cardMsg := platform.TextMessage("").
		WithEmbeds(platform.Embed{
			Title:       "Remilia 图文卡片（Embed 路径）",
			Description: label("A2", "platform.Embed 被自动映射成 tuwen 卡片。若 A1 与 A2 外观一致，说明映射正确。"),
			URL:         linkURL,
			ImageURL:    picURL,
		})
	if res, err := sender.Send(ctx, platform.SendRequest{
		Target:  platform.ChatInfo{ID: target, IsGroup: isGroup},
		Message: cardMsg,
	}); err != nil {
		t.Logf("[%s-A2] 发送失败：%v", scene, err)
	} else {
		t.Logf("[%s-A2] 发送成功 message_id=%s", scene, res.MessageID)
	}

	// A3：对照组 —— 同样的图与文字走 Markdown，便于对比两种渲染。
	md := fmt.Sprintf("## Remilia 图文卡片\n\n%s\n\n![image #600px #338px](%s)\n\n[开发文档](%s)",
		label("A3", "对照组：同一张图+文字走 Markdown。渲染形态应与 A1/A2 明显不同。"),
		picURL, linkURL)
	liveSendRaw(ctx, t, api, target, isGroup, "A3-Markdown对照", &dto.Message{
		Type:     dto.MarkdownMessage,
		Markdown: &dto.Markdown{Content: md},
	})

	// ── B. 素材管理 ─────────────────────────────────────────────────────

	senderImpl, okAssert := sender.(*qqSender)
	if !okAssert {
		t.Fatalf("[%s-B] sender 类型断言失败", scene)
	}

	imgData := makeLiveTestPNG(t)
	img, err := senderImpl.UploadMediaImage(ctx, platform.Attachment{
		Kind:     platform.AttachmentKindImage,
		Name:     "remilia_material.png",
		MimeType: "image/png",
		Data:     imgData,
	})
	if err != nil {
		t.Fatalf("[%s-B1] 素材上传失败：%v", scene, err)
	}
	t.Logf("[%s-B1] 素材上传成功 image_id=%s ttl=%ds url=%s",
		scene, img.ImageID, img.TTL, img.URL)
	if img.TTL == 0 {
		t.Logf("[%s-B1] 警告：ttl 为 0，平台可能未返回滑动有效期", scene)
	}

	// B1：用素材引用发 Markdown —— 关键点是 qqbot-media:// 链接无需公网可达。
	mdMaterial := fmt.Sprintf("## Remilia 素材管理\n\n%s\n\n%s",
		label("B1", "这张图由素材接口上传（qqbot-media:// 引用），不依赖公网图床。"),
		img.MarkdownImage(640, 360))
	liveSendRaw(ctx, t, api, target, isGroup, "B1-素材图片", &dto.Message{
		Type:     dto.MarkdownMessage,
		Markdown: &dto.Markdown{Content: mdMaterial, ForceVerifyImageResource: true},
	})

	// B2：查询列表，确认刚上传的图片在册且有效期被设置。
	images, err := senderImpl.ListMediaImages(ctx, 0)
	if err != nil {
		t.Logf("[%s-B2] 查询素材列表失败：%v", scene, err)
	} else {
		var found bool
		for _, it := range images {
			if it.ImageID == img.ImageID {
				found = true
				t.Logf("[%s-B2] 在列表中：image_id=%s file=%s expires_at=%d",
					scene, it.ImageID, it.FileName, it.ExpiresAt)
				break
			}
		}
		if !found {
			t.Logf("[%s-B2] 警告：刚上传的 image_id=%s 未出现在列表（共 %d 张）", scene, img.ImageID, len(images))
		}
		t.Logf("[%s-B2] 当前有效素材共 %d 张", scene, len(images))
	}

	// B3：清理（默认删除，避免占用 100 张配额）。
	if isTruthy(os.Getenv("QQ_TEST_KEEP_MATERIAL")) {
		t.Logf("[%s-B3] QQ_TEST_KEEP_MATERIAL=true，保留素材 image_id=%s", scene, img.ImageID)
		return
	}
	if err := senderImpl.DeleteMediaImage(ctx, img.ImageID); err != nil {
		t.Logf("[%s-B3] 删除素材失败（可能已被平台清理）：%v", scene, err)
		return
	}
	t.Logf("[%s-B3] 素材已删除 image_id=%s", scene, img.ImageID)
}
