package qq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// ────────────────────────────────────────────────────────────────────────────
// 图文卡片（msg_type=8）
// ────────────────────────────────────────────────────────────────────────────

// TestBuildDTOMessage_CardFromEmbed 验证 platform.Embed 被映射成图文卡片：
// 这是新卡片能力对库使用者的主要入口（跨平台代码写 Embed，QQ 侧自动升级为卡片）。
func TestBuildDTOMessage_CardFromEmbed(t *testing.T) {
	msg := platform.TextMessage("").WithEmbeds(platform.Embed{
		Title:       "今日天气",
		Description: "多云转晴 24~32°C",
		URL:         "https://example.com/weather",
		ImageURL:    "https://example.com/pic.png",
	})
	s := &qqSender{}
	got := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, dto.CardMessage, got.Type)
	require.NotNil(t, got.Card)
	assert.Equal(t, dto.CardTypeTuwen, got.Card.Type)
	assert.Equal(t, "今日天气", got.Card.Content.Title)
	assert.Equal(t, "多云转晴 24~32°C", got.Card.Content.Description)
	assert.Equal(t, "https://example.com/weather", got.Card.Content.URL)
	assert.Equal(t, "https://example.com/pic.png", got.Card.Content.PicURL)
}

// TestBuildDTOMessage_EmbedFallsBackToThumbnail 验证 ImageURL 缺失时用缩略图兜底。
func TestBuildDTOMessage_EmbedFallsBackToThumbnail(t *testing.T) {
	msg := platform.TextMessage("").WithEmbeds(platform.Embed{
		Title:        "标题",
		Description:  "描述",
		URL:          "https://example.com",
		ThumbnailURL: "https://example.com/thumb.png",
	})
	s := &qqSender{}
	got := s.buildDTOMessage(msg, newTestChat())

	require.NotNil(t, got.Card)
	assert.Equal(t, "https://example.com/thumb.png", got.Card.Content.PicURL)
}

// TestBuildDTOMessage_EmbedWithoutURLStaysMarkdown 验证缺链接/图片的 Embed
// 不按卡片发送：平台把四个字段都当必填，硬发会导致整条消息被拒——
// 退回 Markdown 至少正文能送达。
func TestBuildDTOMessage_EmbedWithoutURLStaysMarkdown(t *testing.T) {
	msg := platform.MarkdownMessage("## 正文").WithEmbeds(platform.Embed{
		Title: "只有标题",
	})
	s := &qqSender{}
	got := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, dto.MarkdownMessage, got.Type, "缺 URL/图片时不应使用卡片")
	assert.Nil(t, got.Card)
	assert.Contains(t, got.Markdown.Content, "正文")
}

// TestBuildDTOMessage_EmbedUsesBodyAsDescription 验证 Embed 无 Description 时
// 用正文纯文本兜底（描述是平台必填项，空字符串会被拒）。
func TestBuildDTOMessage_EmbedUsesBodyAsDescription(t *testing.T) {
	msg := platform.OutboundMessage{Text: "来自正文的描述"}.WithEmbeds(platform.Embed{
		Title:    "标题",
		URL:      "https://example.com",
		ImageURL: "https://example.com/pic.png",
	})
	s := &qqSender{}
	got := s.buildDTOMessage(msg, newTestChat())

	require.NotNil(t, got.Card)
	assert.Equal(t, "来自正文的描述", got.Card.Content.Description)
}

// TestBuildDTOMessage_CardTruncatesToPlatformLimits 验证超长字段被截断，
// 而不是原样发给平台（title>50 / desc>200 会被 304061 拒绝）。
func TestBuildDTOMessage_CardTruncatesToPlatformLimits(t *testing.T) {
	longTitle := ""
	for range dto.CardTitleMaxRunes + 20 {
		longTitle += "题"
	}
	longDesc := ""
	for range dto.CardDescMaxRunes + 50 {
		longDesc += "述"
	}
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{
		Card: dto.NewTuwenCard(longTitle, longDesc, "https://example.com", "https://example.com/p.png"),
	})
	s := &qqSender{}
	got := s.buildDTOMessage(msg, newTestChat())

	require.NotNil(t, got.Card)
	assert.Len(t, []rune(got.Card.Content.Title), dto.CardTitleMaxRunes)
	assert.Len(t, []rune(got.Card.Content.Description), dto.CardDescMaxRunes)
}

// TestBuildDTOMessage_CardDoesNotMutateCallerExtra 验证 Normalize 只作用于副本：
// 调用方可能复用同一个 MessageExtra（如缓存模板），不应被发送逻辑改写。
func TestBuildDTOMessage_CardDoesNotMutateCallerExtra(t *testing.T) {
	card := dto.NewTuwenCard(" 待裁剪 ", "描述", "https://example.com", "https://example.com/p.png")
	msg := platform.TextMessage("")
	msg = ApplyExtra(msg, MessageExtra{Card: card})
	s := &qqSender{}

	got := s.buildDTOMessage(msg, newTestChat())
	require.NotNil(t, got.Card)
	assert.Equal(t, "待裁剪", got.Card.Content.Title, "发送的副本应被裁剪")
	assert.Equal(t, " 待裁剪 ", card.Content.Title, "调用方持有的对象不应被修改")
}

// TestBuildDTOMessage_CardDropsButtonsAndMentions 验证卡片消息不附带
// keyboard / AT 标签：卡片没有正文通道，塞进去只会被平台忽略或触发参数错误。
func TestBuildDTOMessage_CardDropsButtonsAndMentions(t *testing.T) {
	msg := platform.TextMessage("正文").
		WithMentions("user_1").
		WithButtons(platform.Button{ID: "b1", Label: "按钮"})
	msg = ApplyExtra(msg, MessageExtra{
		Card: dto.NewTuwenCard("标题", "描述", "https://example.com", "https://example.com/p.png"),
	})
	s := &qqSender{}
	got := s.buildDTOMessage(msg, newTestChat())

	assert.EqualValues(t, dto.CardMessage, got.Type)
	assert.Empty(t, got.Keyboard, "卡片消息不应携带 keyboard")
	assert.NotContains(t, got.Content, "qqbot-at-user")
}

// TestBuildDTOMessage_CardSendsInGroupAndC2C 验证卡片在群聊与单聊都走通：
// 2026-10-28 的更新把图文卡片扩展到了单聊（此前仅群聊文档中有）。
func TestBuildDTOMessage_CardSendsInGroupAndC2C(t *testing.T) {
	for _, isGroup := range []bool{true, false} {
		msg := platform.TextMessage("").WithEmbeds(platform.Embed{
			Title:       "标题",
			Description: "描述",
			URL:         "https://example.com",
			ImageURL:    "https://example.com/p.png",
		})
		s := &qqSender{}
		got := s.buildDTOMessage(msg, platform.ChatInfo{ID: "t", IsGroup: isGroup})
		assert.EqualValues(t, dto.CardMessage, got.Type, "isGroup=%t", isGroup)
	}
}

// TestCardNormalize 覆盖 Normalize 的必填项判定与类型补全。
func TestCardNormalize(t *testing.T) {
	cases := []struct {
		name string
		card dto.Card
		want bool
	}{
		{"完整", dto.Card{Type: dto.CardTypeTuwen, Content: dto.CardContent{
			Title: "t", Description: "d", URL: "https://a", PicURL: "https://b",
		}}, true},
		{"缺标题", dto.Card{Content: dto.CardContent{
			Description: "d", URL: "https://a", PicURL: "https://b",
		}}, false},
		{"缺描述", dto.Card{Content: dto.CardContent{
			Title: "t", URL: "https://a", PicURL: "https://b",
		}}, false},
		{"缺链接", dto.Card{Content: dto.CardContent{
			Title: "t", Description: "d", PicURL: "https://b",
		}}, false},
		{"缺图片", dto.Card{Content: dto.CardContent{
			Title: "t", Description: "d", URL: "https://a",
		}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := tc.card
			assert.Equal(t, tc.want, card.Normalize())
			if tc.want {
				assert.Equal(t, dto.CardTypeTuwen, card.Type, "空 type 应补为 tuwen")
			}
		})
	}

	var nilCard *dto.Card
	assert.False(t, nilCard.Normalize(), "nil 卡片不可发送")
}

// TestQQCapabilities_Embeds 验证能力声明与实现一致（Embeds 现在为 true）。
func TestQQCapabilities_Embeds(t *testing.T) {
	caps := qqCapabilities()
	assert.True(t, caps.Embeds)
	assert.True(t, caps.Has(platform.CapEmbeds))
}

// ────────────────────────────────────────────────────────────────────────────
// 素材管理
// ────────────────────────────────────────────────────────────────────────────

// fakeMaterialAPI 模拟素材管理三步流程，并可记录 PUT 预签名 URL 的请求。
type fakeMaterialAPI struct {
	openapi.OpenAPI

	prepareReq  *dto.MediaImagePrepareRequest
	completeReq *dto.MediaImageCompleteRequest
	prepareResp gjson.Result
	completeRes gjson.Result
	prepareErr  error
	pages       []gjson.Result
	deleted     []string
	cursors     []string
}

func (f *fakeMaterialAPI) PrepareMediaImage(_ context.Context, req *dto.MediaImagePrepareRequest) (gjson.Result, error) {
	f.prepareReq = req
	return f.prepareResp, f.prepareErr
}

func (f *fakeMaterialAPI) CompleteMediaImage(_ context.Context, req *dto.MediaImageCompleteRequest) (gjson.Result, error) {
	f.completeReq = req
	return f.completeRes, nil
}

func (f *fakeMaterialAPI) GetMediaImages(_ context.Context, cursor string, _ int) (gjson.Result, error) {
	f.cursors = append(f.cursors, cursor)
	if len(f.pages) == 0 {
		return gjson.Parse(`{"images":[]}`), nil
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page, nil
}

func (f *fakeMaterialAPI) DeleteMediaImage(_ context.Context, imageID string) (gjson.Result, error) {
	f.deleted = append(f.deleted, imageID)
	return gjson.Parse(`{}`), nil
}

// materialTestPNG 返回最小可用的 PNG 头（足够魔数嗅探，不需要合法图像）。
func materialTestPNG() []byte {
	return append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, []byte("payload")...)
}

// TestUploadMediaImage_ThreeStepFlow 验证 prepare → PUT → complete 全链路，
// 且 complete 使用 PUT 响应头里的 ETag（不是本地算的 MD5）。
func TestUploadMediaImage_ThreeStepFlow(t *testing.T) {
	var (
		mu      sync.Mutex
		putBody []byte
		putCT   string
		putHits int
	)
	presigned := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		putHits++
		putCT = r.Header.Get("Content-Type")
		putBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(putBody)
		w.Header().Set("ETag", `"etag-from-cos"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(presigned.Close)

	api := &fakeMaterialAPI{
		prepareResp: gjson.Parse(`{"upload_id":"up_1","presigned_url":"` + presigned.URL + `","expires_in":3600}`),
		completeRes: gjson.Parse(`{"image_id":"img_1","url":"qqbot-media://image/v1/tok","ttl":7776000,"preview_url":"https://p"}`),
	}
	s := &qqSender{api: api}

	img, err := s.UploadMediaImage(context.Background(), platform.Attachment{
		Kind: platform.AttachmentKindImage,
		Name: "chart.png",
		Data: materialTestPNG(),
	})
	require.NoError(t, err)

	// 返回的引用
	assert.Equal(t, "qqbot-media://image/v1/tok", img.URL)
	assert.Equal(t, "img_1", img.ImageID)
	assert.EqualValues(t, 7776000, img.TTL)
	assert.Equal(t, "https://p", img.PreviewURL)

	// prepare 请求：类型/校验值/文件名
	require.NotNil(t, api.prepareReq)
	assert.Equal(t, "image/png", api.prepareReq.ContentType)
	assert.Equal(t, "chart.png", api.prepareReq.FileName)
	assert.EqualValues(t, len(materialTestPNG()), api.prepareReq.FileSize)
	assert.Len(t, api.prepareReq.MD5, 32)
	assert.Len(t, api.prepareReq.SHA1, 40)

	// PUT：Content-Type 与 prepare 一致，body 为原图
	mu.Lock()
	assert.Equal(t, 1, putHits)
	assert.Equal(t, "image/png", putCT)
	assert.Equal(t, materialTestPNG(), putBody)
	mu.Unlock()

	// complete：md5 用 COS ETag（去引号）
	require.NotNil(t, api.completeReq)
	assert.Equal(t, "up_1", api.completeReq.UploadID)
	assert.Equal(t, "etag-from-cos", api.completeReq.MD5)
}

// TestUploadMediaImage_RejectsUnsupportedType 验证非 png/jpeg/gif/webp 在本地
// 就被拒绝，不发一个必然 40035001 的请求。
func TestUploadMediaImage_RejectsUnsupportedType(t *testing.T) {
	api := &fakeMaterialAPI{}
	s := &qqSender{api: api}

	_, err := s.UploadMediaImage(context.Background(), platform.Attachment{
		Kind:     platform.AttachmentKindFile,
		Name:     "doc.pdf",
		MimeType: "application/pdf",
		Data:     []byte("%PDF-1.7 not an image"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
	assert.Nil(t, api.prepareReq, "不应发出 prepare 请求")
}

// TestUploadMediaImage_RejectsOversize 验证超过 10MB 的图片被本地拒绝。
func TestUploadMediaImage_RejectsOversize(t *testing.T) {
	s := &qqSender{api: &fakeMaterialAPI{}}

	data := make([]byte, materialMaxImageBytes+1)
	copy(data, materialTestPNG())
	_, err := s.UploadMediaImage(context.Background(), platform.Attachment{
		Kind: platform.AttachmentKindImage, Name: "big.png", Data: data,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10MB")
}

// TestUploadMediaImage_RequiresData 验证只给 URL 的附件不能走素材上传
// （素材接口要的是二进制；URL 直接用公网链接即可）。
func TestUploadMediaImage_RequiresData(t *testing.T) {
	s := &qqSender{api: &fakeMaterialAPI{}}
	_, err := s.UploadMediaImage(context.Background(), platform.Attachment{
		Kind: platform.AttachmentKindImage, URL: "https://example.com/a.png",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary data")
}

// TestMaterialImageType_SniffOverridesDeclaredMIME 验证二进制嗅探优先：
// pic 压缩链路会把 PNG 转 JPEG，声明的 MIME 可能是错的。
func TestMaterialImageType_SniffOverridesDeclaredMIME(t *testing.T) {
	jpegMagic := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	ct, ext := materialImageType(platform.Attachment{
		MimeType: "image/png", // 声明错了
		Data:     jpegMagic,   // 实际是 JPEG
	})
	assert.Equal(t, "image/jpeg", ct)
	assert.Equal(t, ".jpg", ext)

	// 无数据时退回声明值，并归一化 image/jpg
	ct, ext = materialImageType(platform.Attachment{MimeType: "image/jpg"})
	assert.Equal(t, "image/jpeg", ct)
	assert.Equal(t, ".jpg", ext)
}

// TestMediaImage_MarkdownImage 验证 MaterialImage 渲染出的 Markdown 图片语法
// 总带尺寸参数（缺尺寸时 QQ 客户端不渲染图片）。
func TestMediaImage_MarkdownImage(t *testing.T) {
	img := MediaImage{URL: "qqbot-media://image/v1/tok"}

	assert.Equal(t, "![image #800px #400px](qqbot-media://image/v1/tok)",
		img.MarkdownImage(800, 400))

	// 尺寸未知时用默认长边，而不是省略尺寸
	assert.Equal(t, "![image #600px #600px](qqbot-media://image/v1/tok)",
		img.MarkdownImage(0, 0))

	assert.Empty(t, MediaImage{}.MarkdownImage(800, 400), "空 URL 应返回空串")
}

// TestListMediaImages_FollowsCursor 验证素材列表按 next_cursor 翻页到底。
func TestListMediaImages_FollowsCursor(t *testing.T) {
	api := &fakeMaterialAPI{pages: []gjson.Result{
		gjson.Parse(`{"images":[{"image_id":"i1","url":"u1","file_name":"a.png","created_at":1,"expires_at":2}],"next_cursor":"c2","total":2}`),
		gjson.Parse(`{"images":[{"image_id":"i2","url":"u2","file_name":"b.png","created_at":3,"expires_at":4}],"next_cursor":"","total":2}`),
	}}
	s := &qqSender{api: api}

	images, err := s.ListMediaImages(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, images, 2)
	assert.Equal(t, "i1", images[0].ImageID)
	assert.Equal(t, "u1", images[0].URL)
	assert.Equal(t, "b.png", images[1].FileName)
	assert.EqualValues(t, 4, images[1].ExpiresAt)
	assert.Equal(t, []string{"", "c2"}, api.cursors, "应使用上一页的 next_cursor")
}

// TestListMediaImages_StopsOnRepeatedCursor 验证游标不前进时终止，
// 避免服务端回环导致死循环。
func TestListMediaImages_StopsOnRepeatedCursor(t *testing.T) {
	page := gjson.Parse(`{"images":[],"next_cursor":"same"}`)
	api := &fakeMaterialAPI{pages: []gjson.Result{page, page, page}}
	s := &qqSender{api: api}

	_, err := s.ListMediaImages(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, api.cursors, 2, "应请求两页后因游标重复而停止")
}

// TestDeleteMediaImage 验证删除转发到平台并拒绝空 ID。
func TestDeleteMediaImage(t *testing.T) {
	api := &fakeMaterialAPI{}
	s := &qqSender{api: api}

	require.NoError(t, s.DeleteMediaImage(context.Background(), "img_1"))
	assert.Equal(t, []string{"img_1"}, api.deleted)

	require.Error(t, s.DeleteMediaImage(context.Background(), ""), "空 image id 应在本地拒绝")
}

// TestUploadMediaImage_NilAPI 验证未注入 API 时不 panic。
func TestUploadMediaImage_NilAPI(t *testing.T) {
	s := &qqSender{}
	_, err := s.UploadMediaImage(context.Background(), platform.Attachment{
		Kind: platform.AttachmentKindImage, Name: "a.png", Data: materialTestPNG(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestMediaImageUploaderInterface 验证 Sender 实现了公开的素材扩展接口，
// 且包级 UploadMediaImage 与 Sender 方法行为一致（同一份实现）。
func TestMediaImageUploaderInterface(t *testing.T) {
	s := NewSender(&fakeMaterialAPI{})

	up, ok := s.(MediaImageUploader)
	require.True(t, ok, "qq Sender 应可通过类型断言取得 MediaImageUploader")

	var _ MediaImageUploader = (*qqSender)(nil)
	assert.NotNil(t, up)
}

// TestUploadMediaImage_StandaloneFunc 验证包级入口（只有 openapi 客户端时可用）。
func TestUploadMediaImage_StandaloneFunc(t *testing.T) {
	presigned := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(presigned.Close)

	api := &fakeMaterialAPI{
		prepareResp: gjson.Parse(`{"upload_id":"u","presigned_url":"` + presigned.URL + `"}`),
		completeRes: gjson.Parse(`{"url":"qqbot-media://image/v1/x","image_id":"i"}`),
	}

	img, err := UploadMediaImage(context.Background(), api, platform.Attachment{
		Kind: platform.AttachmentKindImage, Name: "a.png", Data: materialTestPNG(),
	})
	require.NoError(t, err)
	assert.Equal(t, "qqbot-media://image/v1/x", img.URL)
	assert.Equal(t, "a.png", img.FileName, "应回填实际登记的文件名")

	_, err = UploadMediaImage(context.Background(), nil, platform.Attachment{})
	require.Error(t, err)
}

// TestUploadMediaImage_AppendsExtensionWhenMissing 验证文件名缺扩展名时补上：
// 平台按"扩展名与 MIME 是否匹配"校验，缺扩展名会 40035001。
func TestUploadMediaImage_AppendsExtensionWhenMissing(t *testing.T) {
	presigned := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(presigned.Close)

	api := &fakeMaterialAPI{
		prepareResp: gjson.Parse(`{"upload_id":"u","presigned_url":"` + presigned.URL + `"}`),
		completeRes: gjson.Parse(`{"url":"qqbot-media://image/v1/x"}`),
	}
	_, err := UploadMediaImage(context.Background(), api, platform.Attachment{
		Kind: platform.AttachmentKindImage, Name: "no_ext", Data: materialTestPNG(),
	})
	require.NoError(t, err)
	assert.Equal(t, "no_ext.png", api.prepareReq.FileName)

	// 空文件名时按类型生成默认名
	api2 := &fakeMaterialAPI{
		prepareResp: gjson.Parse(`{"upload_id":"u","presigned_url":"` + presigned.URL + `"}`),
		completeRes: gjson.Parse(`{"url":"qqbot-media://image/v1/x"}`),
	}
	_, err = UploadMediaImage(context.Background(), api2, platform.Attachment{
		Kind: platform.AttachmentKindImage, Data: materialTestPNG(),
	})
	require.NoError(t, err)
	assert.Equal(t, "image.png", api2.prepareReq.FileName)
}

// TestMediaImageWireFormat 固化请求体字段名：素材接口是新增能力，
// 字段拼错不会有编译错误，只会在真机上 400，因此用 JSON 断言锁住。
func TestMediaImageWireFormat(t *testing.T) {
	prep, err := json.Marshal(dto.MediaImagePrepareRequest{
		FileName: "a.png", FileSize: 3, ContentType: "image/png", MD5: "m", SHA1: "s",
	})
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"file_name":"a.png","file_size":3,"content_type":"image/png","md5":"m","sha1":"s"}`,
		string(prep))

	comp, err := json.Marshal(dto.MediaImageCompleteRequest{UploadID: "u", MD5: "m"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"upload_id":"u","md5":"m"}`, string(comp))

	card, err := json.Marshal(dto.NewTuwenCard("t", "d", "https://a", "https://b"))
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"type":"tuwen","content":{"title":"t","description":"d","url":"https://a","pic_url":"https://b"}}`,
		string(card))
}
