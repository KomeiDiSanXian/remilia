//go:build network

// 真机验证：QQ 原生 Markdown（msg_type=2）内嵌图片（`![image #宽px #高px](公网URL)`）
// 能否与正文同条渲染"图片 + Markdown 图文"。
//
// 背景：
//   - sender.sendMarkdownWithImage 对本地图片强制走分片上传换取 raw_url
//     （COS 预签名 GET URL），嵌入 markdown 发送 msg_type=2。
//   - 社区实测：QQ 原生 markdown 图片语法缺少 #宽px #高px 尺寸参数时，
//     消息发出但图片不渲染（只剩文字/链接）。
//
// 本测试发送三种载荷供真机对比：
//
//	A. sender 现行路径：Markdown 正文 + 本地图片 Data 附件（分片上传 → raw_url 内嵌）
//	B. 直接调用接口：Markdown + QQ_TEST_IMAGE_URL 外链图片内嵌（对照组，无上传）
//	C. 直接调用接口：Markdown 纯文本（确认 markdown 权限可用）
//
// 运行（凭据解析同 TestQQMediaImageTextLive）：
//
//	$env:QQ_TEST_GROUP_ID="<群 openid>"   # 群聊场景（可选）
//	$env:QQ_TEST_USER_ID="<单聊 openid>"  # 单聊场景（可选）
//	# 可选：QQ_TEST_IMAGE_URL 指定公网图片 URL（B 场景用；未设置则跳过 B）
//	go test -tags network -run TestQQMarkdownImageLive -v ./platform/qq/
package qq

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
)

// TestQQMarkdownImageLive 真机验证 msg_type=2 markdown 内嵌图片。
func TestQQMarkdownImageLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：go test -tags network -run TestQQMarkdownImageLive -v ./platform/qq/")
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
			runMarkdownImageScenario(t, api, groupID, true, imgData)
		})
	}
	if userID != "" {
		t.Run("单聊", func(t *testing.T) {
			runMarkdownImageScenario(t, api, userID, false, imgData)
		})
	}
}

func runMarkdownImageScenario(t *testing.T, api openapi.OpenAPI, target string, isGroup bool, imgData []byte) {
	t.Helper()
	scene := "群聊"
	if !isGroup {
		scene = "单聊"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	stamp := time.Now().Format("15:04:05.000")
	sender := NewSender(api)
	chat := platform.ChatInfo{ID: target, IsGroup: isGroup}

	// A1：sender 现行路径 —— Markdown 正文 + 本地图片 Data 附件
	// （分片上传 → raw_url → ![image #宽px #高px](raw_url) 内嵌）
	mdA := fmt.Sprintf("## %s A1 上传raw_url\n破损图排查：分片上传换取的 COS raw_url。", stamp)
	resA, errA := sender.Send(ctx, platform.SendRequest{
		Target: chat,
		Message: platform.OutboundMessage{Markdown: mdA}.WithAttachments(platform.Attachment{
			Kind:     platform.AttachmentKindImage,
			Name:     "md_live.png",
			MimeType: "image/png",
			Data:     imgData,
		}),
	})
	if errA != nil {
		t.Logf("[%s-A1] 发送失败：%v", scene, errA)
	} else {
		t.Logf("[%s-A1] 已发送，message_id=%s", scene, resA.MessageID)
	}

	// A2：手动分片上传，把 raw_url 打到日志并立即本地核验可达性与类型
	// （对象 Content-Type 由分片 PUT 时的请求头写入 COS 元数据）。
	imgAtt := platform.Attachment{Kind: platform.AttachmentKindImage, Name: "probe.png", MimeType: "image/png", Data: imgData}
	senderImpl := sender.(*qqSender)
	if uploadResult, err := senderImpl.uploadChunked(ctx, chat, imgAtt); err != nil {
		t.Logf("[%s-A2] 分片上传失败：%v", scene, err)
	} else {
		rawURL := uploadResult.Get("raw_url").String()
		t.Logf("[%s-A2] raw_url=%s", scene, rawURL)
		if rawURL != "" {
			if resp, gerr := http.Get(rawURL); gerr == nil {
				defer resp.Body.Close()
				t.Logf("[%s-A2] 本地核验：HTTP %d, Content-Type=%s, Length=%d",
					scene, resp.StatusCode, resp.Header.Get("Content-Type"), resp.ContentLength)
			} else {
				t.Logf("[%s-A2] 本地核验失败：%v", scene, gerr)
			}
		}
	}

	// B1：官方文档示例图片（腾讯 COS 公网 URL，无查询参数）——验证"任意公网 URL"是否可渲染
	officialImgURL := "https://resource5-1255303497.cos.ap-guangzhou.myqcloud.com/abcmouse_word_watch/markdown/building.png"
	md := fmt.Sprintf("## %s B1 官方示例图\n无查询参数的公网 URL 对照。\n\n![image #208px #320px](%s)", stamp, officialImgURL)
	liveSendRaw(ctx, t, api, target, isGroup, "B1-COS公网URL", &dto.Message{
		Type:     dto.MarkdownMessage,
		Markdown: &dto.Markdown{Content: md},
	})

	// B2：带查询参数的公网 URL —— 验证 markdown 解析器是否会截断 query string
	md = fmt.Sprintf("## %s B2 带查询参数\nURL 含 ?a=1&b=2，若破损说明 query 被截断。\n\n![image #208px #320px](https://www.baidu.com/img/PCtm_d9c8750bec047d692d4c9c6340dbe0a.png?x=1&y=2)", stamp)
	liveSendRaw(ctx, t, api, target, isGroup, "B2-带query", &dto.Message{
		Type:     dto.MarkdownMessage,
		Markdown: &dto.Markdown{Content: md},
	})

	// C：Markdown 纯文本 —— 权限与基础渲染对照（已知正常，保留回归基准）
	md = fmt.Sprintf("## %s C 纯文本对照\n**加粗** 与 [链接](https://www.qq.com) 应正常渲染。", stamp)
	liveSendRaw(ctx, t, api, target, isGroup, "C-Markdown纯文本", &dto.Message{
		Type:     dto.MarkdownMessage,
		Markdown: &dto.Markdown{Content: md},
	})
}
