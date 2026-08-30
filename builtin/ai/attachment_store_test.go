package ai

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/messagelog"
	"github.com/KomeiDiSanXian/remilia/builtin/messagelog/attachments"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// newStoreTestLogger 创建带附件文件存储的测试 messagelog。
// 附件行直接插入 DB（绕过异步 flush），确保测试确定性。
func newStoreTestLogger(t *testing.T, storeDir string) (*messagelog.Logger, *attachments.FileStore) {
	t.Helper()
	store, err := attachments.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	db, err := messagelog.OpenDB(filepath.Join(t.TempDir(), "ml.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	l := messagelog.New(10)
	l.UseDB(db)
	l.Start(messagelog.Options{
		Attachments: &messagelog.AttachmentOptions{
			Dir:     storeDir,
			Scope:   "none",
			MaxSize: 1 << 20,
		},
	})
	t.Cleanup(func() {
		l.Stop()
		if sqlDB, err := db.DB(); err == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return l, store
}

// insertStoredImage 直插一条消息记录 + ready 附件行，二进制写入文件存储。
// 返回被引用消息的 platform_message_id。
func insertStoredImage(t *testing.T, l *messagelog.Logger, store *attachments.FileStore, chatID, eventID string) string {
	t.Helper()
	data := "fake-image-bytes-" + eventID
	key, _, err := store.Put(strings.NewReader(data), 1<<20)
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}
	now := time.Now().UnixNano()
	if err := l.DB().Create(&messagelog.MessageRecord{
		Platform: "qq", Kind: "GROUP_MESSAGE", EventID: eventID,
		PlatformMessageID: eventID, ChatID: chatID, UserID: "u1", UserName: "小明",
		Content: "这是一张图", Timestamp: now, CreatedAt: now, IsGroup: true,
	}).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if err := l.DB().Create(&messagelog.AttachmentRecord{
		EventID: eventID, Type: "image", Name: "a.png", MimeType: "image/png",
		URL: "http://example.com/a.png", Status: "ready", StorageKey: key,
	}).Error; err != nil {
		t.Fatalf("insert attachment row: %v", err)
	}
	return eventID
}

// TestQuotedImagePartFromStore 引用消息的图片优先从 messagelog 附件存储读取
// （URL 过期免疫），而非段提取直链。
func TestQuotedImagePartFromStore(t *testing.T) {
	storeDir := t.TempDir() + "/att"
	l, store := newStoreTestLogger(t, storeDir)
	insertStoredImage(t, l, store, "g1", "in-1")

	p := &Plugin{cfg: &Config{VisionEnabled: true}, history: l}
	evt := &replyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "这张图里的猫叫什么",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
			platform.WithSyntheticSender(platform.UserInfo{ID: "u2", DisplayName: "小红"})),
		replyID: "in-1",
	}
	ctx := eventctx.NewContextFromEvent(evt, nil)
	session := &Session{ID: "s1", UserID: "u2", ChatID: "g1"}

	cp := p.quotedImagePart(ctx, session)
	if cp == nil {
		t.Fatal("expected quoted image from store, got nil")
	}
	if cp.Type != ContentPartImage {
		t.Fatalf("expected image part, got type %q", cp.Type)
	}
	if string(cp.Data) != "fake-image-bytes-in-1" {
		t.Errorf("expected stored binary, got %q", cp.Data)
	}
	if cp.SourceURL != "http://example.com/a.png" {
		t.Errorf("expected source URL from stored row, got %q", cp.SourceURL)
	}

	// 二次读取命中会话缓存（同一 URL），仍返回相同内容
	cp2 := p.quotedImagePart(ctx, session)
	if cp2 == nil || string(cp2.Data) != "fake-image-bytes-in-1" {
		t.Errorf("expected cached stored image, got %+v", cp2)
	}
}

// TestQuotedImagePartFromStoreFallback 存储未落库（无附件行）时回退段直链，
// 且不会 panic（history 为 nil 时同样安全）。
func TestQuotedImagePartFromStoreFallback(t *testing.T) {
	// 存储可用但被引用消息无附件行：quotedImageFromSegments 返回空 URL → nil
	storeDir := t.TempDir() + "/att"
	l, _ := newStoreTestLogger(t, storeDir)
	now := time.Now().UnixNano()
	if err := l.DB().Create(&messagelog.MessageRecord{
		Platform: "qq", Kind: "GROUP_MESSAGE", EventID: "in-2",
		PlatformMessageID: "in-2", ChatID: "g1", UserID: "u1", UserName: "小明",
		Content: "纯文本", Timestamp: now, CreatedAt: now, IsGroup: true,
	}).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}
	p := &Plugin{cfg: &Config{VisionEnabled: true}, history: l}
	evt := &replyEvent{
		Event: platform.NewSyntheticEvent(platform.EventKindGroupMessage, "在吗",
			platform.WithSyntheticChat(platform.ChatInfo{ID: "g1", IsGroup: true})),
		replyID: "in-2",
	}
	if cp := p.quotedImagePart(eventctx.NewContextFromEvent(evt, nil), &Session{}); cp != nil {
		t.Errorf("expected nil for quoted message without attachments, got %+v", cp)
	}

	// history 为 nil：不 panic，直接回退段直链路径
	p2 := &Plugin{cfg: &Config{VisionEnabled: true}, history: nil}
	if cp := p2.quotedImagePart(eventctx.NewContextFromEvent(evt, nil), &Session{}); cp != nil {
		t.Errorf("expected nil with nil history, got %+v", cp)
	}
}
