//go:build network

// 真机验证：QQ 富媒体消息（msg_type=7）能否与 content 同时携带，实现"图片+文字一条消息"。
//
// 背景：
//   - sender.go 的 qqSegmentsToFlat / sendMediaMessage 长期按"富媒体不可混排文本"处理：
//     单聊（C2C）直接把 dtoMsg.Content 清空，群聊仅在正文为空时补一个空格占位。
//   - 真机验证（2026-09，C2C/群聊）结论：msg_type=7 可同时携带 media 与 content，QQ 端
//     会把图片与正文渲染在同一条消息里。官方文档只给
//     {"msg_type":7,"media":{"file_info":"..."}} 示例，因此保留三种载荷作为回归对照。
//
// 本测试向同一目标发送三种载荷：
//
//	A. 直接调用发消息接口：msg_type=7 + media + content（非空文本）——候选"图文混排"
//	B. 直接调用发消息接口：msg_type=7 + media，不带 content（官方示例形态，对照组）
//	C. 走 qqSender.Send 现行段路径（文本段+图片段），复现生产代码实际发出的载荷
//
// 运行（PowerShell，从仓库根目录执行；凭据不写死在代码里）：
//
// 凭据解析顺序：环境变量 QQ_APP_ID / QQ_APP_SECRET 优先；未设置时回退读取
// 仓库根目录 config.yaml 的 bot.qq.app_id / bot.qq.secret
// （go test 的工作目录是包目录 platform/qq，根目录配置文件即 ../../config.yaml）。
//
//	$env:QQ_TEST_GROUP_ID="<群 openid>"       # 群聊场景（可选，与单聊可同时设置）
//	$env:QQ_TEST_USER_ID="<单聊 openid>"      # 单聊场景（可选）
//	# 也可以只用旧式单目标写法（二选一）：
//	#   $env:QQ_TEST_TARGET="<openid>"
//	#   $env:QQ_TEST_IS_GROUP="true"          # "true"/"1" 表示群聊，否则按单聊
//	go test -tags network -run TestQQMediaImageTextLive -v ./platform/qq/
//
// 可选：QQ_TEST_IMAGE_URL 指定公网可访问的图片 URL（QQ 服务器侧抓取）；默认在测试内
// 生成一张 PNG 并以 base64 file_data 上传，不依赖外部图床，也避免海外 URL 在 QQ
// 服务器侧不可达。
package qq

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v3"
)

// TestQQMediaImageTextLive 真机验证 msg_type=7 是否支持图文混排。
func TestQQMediaImageTextLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：go test -tags network -run TestQQMediaImageTextLive -v ./platform/qq/")
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
			runLiveScenario(t, api, groupID, true, imgData)
		})
	}
	if userID != "" {
		t.Run("单聊", func(t *testing.T) {
			runLiveScenario(t, api, userID, false, imgData)
		})
	}
}

// resolveLiveTargets 解析测试目标：优先 QQ_TEST_GROUP_ID / QQ_TEST_USER_ID，
// 兼容 QQ_TEST_TARGET + QQ_TEST_IS_GROUP 的旧式写法。两者都未配置时跳过。
func resolveLiveTargets(t *testing.T) (groupID, userID string) {
	t.Helper()
	groupID = os.Getenv("QQ_TEST_GROUP_ID")
	userID = os.Getenv("QQ_TEST_USER_ID")
	if groupID == "" && userID == "" {
		if target := os.Getenv("QQ_TEST_TARGET"); target != "" {
			if isTruthy(os.Getenv("QQ_TEST_IS_GROUP")) {
				groupID = target
			} else {
				userID = target
			}
		}
	}
	if groupID == "" && userID == "" {
		t.Skipf("未配置测试目标：设置 QQ_TEST_GROUP_ID 和/或 QQ_TEST_USER_ID（或用 QQ_TEST_TARGET + QQ_TEST_IS_GROUP）")
	}
	return groupID, userID
}

// isTruthy 解析 "true"/"1" 等布尔环境变量。
func isTruthy(v string) bool {
	return strings.EqualFold(v, "true") || v == "1"
}

// liveCredentials 解析测试凭据：优先环境变量 QQ_APP_ID / QQ_APP_SECRET，
// 未设置时回退读取仓库根目录 config.yaml 的 bot.qq 区块。
func liveCredentials(t *testing.T) (appID uint64, appSecret string, ok bool) {
	t.Helper()
	if raw := os.Getenv("QQ_APP_ID"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		require.NoError(t, err, "QQ_APP_ID 必须是纯数字 AppID")
		if secret := os.Getenv("QQ_APP_SECRET"); secret != "" {
			return parsed, secret, true
		}
	}
	for _, path := range []string{"../../config.yaml", "config.yaml"} {
		var doc struct {
			Bot struct {
				QQ struct {
					AppID  uint64 `yaml:"app_id"`
					Secret string `yaml:"secret"`
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
		if doc.Bot.QQ.AppID != 0 && doc.Bot.QQ.Secret != "" {
			t.Logf("凭据取自 %s 的 bot.qq 区块（可用 QQ_APP_ID / QQ_APP_SECRET 覆盖）", path)
			return doc.Bot.QQ.AppID, doc.Bot.QQ.Secret, true
		}
	}
	return 0, "", false
}

// runLiveScenario 在单个场景（群聊或单聊）中完成上传与三种载荷发送。
func runLiveScenario(t *testing.T, api openapi.OpenAPI, target string, isGroup bool, imgData []byte) {
	t.Helper()
	scene := "群聊"
	if !isGroup {
		scene = "单聊"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	fileInfo := liveUploadMedia(ctx, t, api, target, isGroup, imgData)
	stamp := time.Now().Format("15:04:05.000")
	text := func(caseID, body string) string {
		return fmt.Sprintf("[%s-%s] %s %s", scene, caseID, stamp, body)
	}

	// A：msg_type=7 + media + content —— 候选"图文混排"载荷（外部开发者主张的形态）
	liveSendRaw(ctx, t, api, target, isGroup, "A-图文混排", &dto.Message{
		Type:    dto.MediaMessage,
		Media:   &dto.MediaResponse{FileInfo: fileInfo},
		Content: text("A", "这条消息尝试让图片和这段文字在同一条消息里显示。若 QQ 端能看到这段文字，说明 msg_type=7 支持携带 content 图文混排。"),
	})

	// B：msg_type=7 + media，不带 content —— 官方示例形态（对照组）
	liveSendRaw(ctx, t, api, target, isGroup, "B-纯媒体对照", &dto.Message{
		Type:  dto.MediaMessage,
		Media: &dto.MediaResponse{FileInfo: fileInfo},
	})

	// C：qqSender 现行段路径（文本段+图片段）—— 复现生产代码实际发出的载荷
	rec := &captureOpenAPI{OpenAPI: api}
	sender := NewSender(rec)
	body := text("C", "这条消息由现行 sender 路径发送（文本段+图片段），与 A 对比用于确认文字是否被吞。")
	sendResult, err := sender.Send(ctx, platform.SendRequest{
		Target: platform.ChatInfo{ID: target, IsGroup: isGroup},
		Message: platform.OutboundMessage{
			Segments: []platform.Segment{
				{Type: platform.SegmentText, Text: body},
				{Type: platform.SegmentImage, Attachment: platform.Attachment{
					Kind: platform.AttachmentKindImage,
					Data: imgData,
				}},
			},
		},
	})
	if got := rec.capture(); got != nil {
		payload, _ := json.Marshal(got)
		t.Logf("%s 用例 C 现行 sender 实际发出 payload=%s", scene, payload)
		if !isGroup && got.Content == "" && body != "" {
			t.Logf("%s 用例 C 注意：现行 sendMediaMessage 在单聊媒体消息里把 content 清空（sendMediaMessage 逻辑），这是当前版本单聊文字被吞的直接原因。", scene)
		}
	}
	if err != nil {
		t.Logf("%s 用例 C 现行 sender 发送失败：%v", scene, err)
		return
	}
	t.Logf("%s 用例 C 发送成功 id=%s", scene, sendResult.MessageID)

	t.Logf("提示：请到 QQ 端查看 %s 中刚发出的 A/B/C 三条消息（同一张图）。修复后 A/C 应同时显示图片与文字（msg_type=7 图文混排），B 仅显示图片；若 A/C 文字缺失说明出现回归。", scene)
}

// liveUploadMedia 上传测试图片并返回 file_info（消息发送阶段的 media.file_info）。
func liveUploadMedia(ctx context.Context, t *testing.T, api openapi.OpenAPI, target string, isGroup bool, imgData []byte) string {
	t.Helper()
	media := &dto.Media{Type: dto.ImageFile}
	if url := os.Getenv("QQ_TEST_IMAGE_URL"); url != "" {
		media.URL = url
	} else {
		media.FileData = base64.StdEncoding.EncodeToString(imgData)
	}

	scene := "群聊"
	if !isGroup {
		scene = "单聊"
	}
	var (
		resp gjson.Result
		err  error
	)
	if isGroup {
		resp, err = api.GroupRichMedia(ctx, target, media)
	} else {
		resp, err = api.SingleRichMedia(ctx, target, media)
	}
	if err != nil {
		t.Fatalf("%s 媒体上传失败（请检查 QQ_APP_ID / QQ_APP_SECRET / 目标 ID）：%v", scene, err)
	}
	fileInfo := resp.Get("file_info").String()
	t.Logf("%s 上传成功，file_info=%q，原始响应=%s", scene, fileInfo, resp.Raw)
	if fileInfo == "" {
		t.Fatalf("%s 上传未返回 file_info：%s", scene, resp.Raw)
	}
	return fileInfo
}

// liveSendRaw 直接调用发消息接口发送一条 dto.Message，并打印载荷与完整响应。
func liveSendRaw(ctx context.Context, t *testing.T, api openapi.OpenAPI, target string, isGroup bool, label string, msg *dto.Message) {
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
		// 平台拒绝（参数不支持 / 主动消息频次或权限受限等）同样是实测结论，仅记录错误码。
		t.Logf("%s 用例 %s 发送失败：%v", scene, label, err)
		return
	}
	t.Logf("%s 用例 %s 发送成功 id=%s，原始响应=%s", scene, label, resp.Get("id").String(), resp.Raw)
}

// captureOpenAPI 包装真实 OpenAPI 客户端，记录经由它发送的最后一条 dto.Message，
// 用于打印 qqSender 现行路径实际发出的载荷（确认单聊是否真的把 content 清空）。
type captureOpenAPI struct {
	openapi.OpenAPI
	mu   sync.Mutex
	last *dto.Message
}

func (c *captureOpenAPI) capture() *dto.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *captureOpenAPI) SingleChat(ctx context.Context, openid string, msg *dto.Message) (gjson.Result, error) {
	c.mu.Lock()
	c.last = msg
	c.mu.Unlock()
	return c.OpenAPI.SingleChat(ctx, openid, msg)
}

func (c *captureOpenAPI) GroupChat(ctx context.Context, groupID string, msg *dto.Message) (gjson.Result, error) {
	c.mu.Lock()
	c.last = msg
	c.mu.Unlock()
	return c.OpenAPI.GroupChat(ctx, groupID, msg)
}

// makeLiveTestPNG 生成一张带斜向条纹的渐变测试图，确保 QQ 端肉眼可辨识。
func makeLiveTestPNG(t *testing.T) []byte {
	t.Helper()
	const width, height = 640, 360
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint8(x * 255 / width)
			g := uint8(y * 255 / height)
			b := uint8(80)
			if (x+y)/32%2 == 0 {
				r, g = 255-r, 255-g
			}
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}
