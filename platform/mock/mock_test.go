package mock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/mock"
)

func TestNewAdapter(t *testing.T) {
	a := mock.NewAdapter()
	if a == nil {
		t.Fatal("NewAdapter returned nil")
	}
	if a.Platform() != "mock" {
		t.Errorf("expected platform %q, got %q", "mock", a.Platform())
	}
	if a.IsRunning() {
		t.Error("new adapter should not be running")
	}
}

func TestAdapterWithOptions(t *testing.T) {
	a := mock.NewAdapter(
		mock.WithPlatform("discord"),
		mock.WithBotID("bot_123"),
		mock.WithBotName("TestBot"),
	)
	if a.Platform() != "discord" {
		t.Errorf("expected %q, got %q", "discord", a.Platform())
	}
	if a.BotID() != "bot_123" {
		t.Errorf("expected %q, got %q", "bot_123", a.BotID())
	}
	if a.BotName() != "TestBot" {
		t.Errorf("expected %q, got %q", "TestBot", a.BotName())
	}
}

func TestAdapterStartStop(t *testing.T) {
	a := mock.NewAdapter()
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := a.Start(ctx, func(e platform.Event) {})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
	if a.CalledTimes("Start") != 1 {
		t.Errorf("expected Start called once, got %d", a.CalledTimes("Start"))
	}
}

func TestAdapterStartWithError(t *testing.T) {
	expectedErr := errors.New("start failed")
	a := mock.NewAdapter(mock.WithStartError(expectedErr))

	err := a.Start(context.Background(), func(e platform.Event) {})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestAdapterStop(t *testing.T) {
	a := mock.NewAdapter()
	err := a.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if a.CalledTimes("Stop") != 1 {
		t.Errorf("expected Stop called once, got %d", a.CalledTimes("Stop"))
	}
}

func TestAdapterStopWithError(t *testing.T) {
	expectedErr := errors.New("stop failed")
	a := mock.NewAdapter(mock.WithStopError(expectedErr))

	err := a.Stop(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestAdapterInjectEvent(t *testing.T) {
	a := mock.NewAdapter()
	received := make(chan platform.Event, 1)

	ctx, cancel := context.WithCancel(context.Background())
	go a.Start(ctx, func(e platform.Event) {
		received <- e
	})
	time.Sleep(5 * time.Millisecond)

	event := platform.NewSyntheticEvent("PRIVATE_MESSAGE", "hello")
	if !a.InjectEvent(event) {
		t.Fatal("InjectEvent returned false (adapter not started)")
	}

	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	cancel()
}

func TestAdapterInjectEventNotStarted(t *testing.T) {
	a := mock.NewAdapter()
	event := platform.NewSyntheticEvent("PRIVATE_MESSAGE", "hello")
	if a.InjectEvent(event) {
		t.Error("InjectEvent should return false when adapter not started")
	}
}

func TestAdapterInterface(t *testing.T) {
	a := mock.NewAdapter()
	var adapter platform.Adapter = a
	_ = adapter

	if a.BotID() != "" {
		t.Error("expected empty bot id by default")
	}
	if _, ok := any(a).(platform.BotIdentity); !ok {
		t.Error("Adapter should implement BotIdentity")
	}
}

func TestAdapterRecoverable(t *testing.T) {
	a := mock.NewAdapter()
	if _, ok := any(a).(platform.RecoverableAdapter); !ok {
		t.Error("Adapter should implement RecoverableAdapter")
	}
}

func TestAdapterHealthDetailer(t *testing.T) {
	a := mock.NewAdapter(mock.WithHealthDetail(map[string]any{"status": "ok"}))
	if _, ok := any(a).(platform.HealthDetailer); !ok {
		t.Error("Adapter should implement HealthDetailer")
	} else {
		if a.HealthDetail()["status"] != "ok" {
			t.Errorf("expected status=ok, got %v", a.HealthDetail()["status"])
		}
	}
}

func TestAdapterDisconnect(t *testing.T) {
	a := mock.NewAdapter()
	notified := make(chan error, 1)
	a.OnDisconnect(func(err error) {
		notified <- err
	})
	a.NotifyDisconnect(errors.New("connection lost"))

	select {
	case err := <-notified:
		if err.Error() != "connection lost" {
			t.Errorf("expected 'connection lost', got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for disconnect notification")
	}
}

func TestNewSender(t *testing.T) {
	s := mock.NewSender()
	if s == nil {
		t.Fatal("NewSender returned nil")
	}
}

func TestSenderSend(t *testing.T) {
	s := mock.NewSender()
	result, err := s.Send(context.Background(), platform.SendRequest{
		Target:  platform.ChatInfo{ID: "chat_1"},
		Message: platform.OutboundMessage{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if s.CalledTimes("Send") != 1 {
		t.Errorf("expected Send called once, got %d", s.CalledTimes("Send"))
	}
	_ = result
}

func TestSenderSendWithError(t *testing.T) {
	expectedErr := errors.New("send failed")
	s := mock.NewSender(mock.WithSendError(expectedErr))

	_, err := s.Send(context.Background(), platform.SendRequest{
		Target:  platform.ChatInfo{ID: "chat_1"},
		Message: platform.OutboundMessage{Text: "hi"},
	})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestSenderEditor(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.MessageEditor); !ok {
		t.Fatal("MockSender should implement MessageEditor")
	}
	err := s.Edit(context.Background(), "chat_1", "msg_1", platform.OutboundMessage{Text: "edited"})
	if err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
	if s.CalledTimes("Edit") != 1 {
		t.Errorf("expected Edit called once, got %d", s.CalledTimes("Edit"))
	}
}

func TestSenderDeleter(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.MessageDeleter); !ok {
		t.Fatal("MockSender should implement MessageDeleter")
	}
	err := s.Delete(context.Background(), "chat_1", "msg_1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestSenderReaction(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.ReactionSender); !ok {
		t.Fatal("MockSender should implement ReactionSender")
	}
	emoji := platform.Emoji{Kind: platform.EmojiKindUnicode, Value: "👍"}
	err := s.AddReaction(context.Background(), "chat_1", "msg_1", emoji)
	if err != nil {
		t.Fatalf("AddReaction failed: %v", err)
	}
	err = s.RemoveReaction(context.Background(), "chat_1", "msg_1", emoji)
	if err != nil {
		t.Fatalf("RemoveReaction failed: %v", err)
	}
}

func TestSenderTyping(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.TypingNotifier); !ok {
		t.Fatal("MockSender should implement TypingNotifier")
	}
	err := s.SendTyping(context.Background(), "chat_1")
	if err != nil {
		t.Fatalf("SendTyping failed: %v", err)
	}
}

func TestSenderGroupManager(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.GroupManager); !ok {
		t.Fatal("MockSender should implement GroupManager")
	}
	if err := s.KickMember(context.Background(), "g1", "u1", false); err != nil {
		t.Fatalf("KickMember failed: %v", err)
	}
	if err := s.BanMember(context.Background(), "g1", "u1", time.Minute); err != nil {
		t.Fatalf("BanMember failed: %v", err)
	}
	if err := s.SetAdmin(context.Background(), "g1", "u1", true); err != nil {
		t.Fatalf("SetAdmin failed: %v", err)
	}
}

func TestSenderInvitationHandler(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.InvitationHandler); !ok {
		t.Fatal("MockSender should implement InvitationHandler")
	}
}

func TestSenderAutoModerator(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.AutoModerator); !ok {
		t.Fatal("MockSender should implement AutoModerator")
	}
}

func TestSenderGroupInfoProvider(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.GroupInfoProvider); !ok {
		t.Fatal("MockSender should implement GroupInfoProvider")
	}
	info, err := s.GetGroupInfo(context.Background(), "g1")
	if err != nil {
		t.Fatalf("GetGroupInfo failed: %v", err)
	}
	if info.ID != "g1" {
		t.Errorf("expected group ID %q, got %q", "g1", info.ID)
	}
}

func TestSenderAvatarProvider(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.AvatarProvider); !ok {
		t.Fatal("MockSender should implement AvatarProvider")
	}
}

func TestSenderSessionNotifier(t *testing.T) {
	s := mock.NewSender()
	if _, ok := any(s).(platform.SessionNotifier); !ok {
		t.Fatal("MockSender should implement SessionNotifier")
	}
}

func TestSenderResetCalls(t *testing.T) {
	s := mock.NewSender()
	s.Send(context.Background(), platform.SendRequest{Target: platform.ChatInfo{ID: "x"}, Message: platform.OutboundMessage{Text: "a"}})
	s.Send(context.Background(), platform.SendRequest{Target: platform.ChatInfo{ID: "y"}, Message: platform.OutboundMessage{Text: "b"}})
	if s.CalledTimes("Send") != 2 {
		t.Errorf("expected 2 sends, got %d", s.CalledTimes("Send"))
	}
	s.ResetCalls()
	if s.CalledTimes("Send") != 0 {
		t.Error("expected 0 sends after reset")
	}
}

func TestAdapterResetCalls(t *testing.T) {
	a := mock.NewAdapter()
	a.Stop(context.Background())
	a.Stop(context.Background())
	if a.CalledTimes("Stop") != 2 {
		t.Errorf("expected 2 stops, got %d", a.CalledTimes("Stop"))
	}
	a.ResetCalls()
	if a.CalledTimes("Stop") != 0 {
		t.Error("expected 0 stops after reset")
	}
}

func TestAdapterInterfaceSatisfaction(t *testing.T) {
	a := mock.NewAdapter(mock.WithSender(mock.NewSender()))
	// Compile-time checks
	var _ platform.Adapter = a
	var _ platform.RecoverableAdapter = a
	var _ platform.BotIdentity = a
	var _ platform.HealthDetailer = a

	s := mock.NewSender()
	var _ platform.Sender = s
	var _ platform.MessageEditor = s
	var _ platform.MessageDeleter = s
	var _ platform.ReactionSender = s
	var _ platform.TypingNotifier = s
	var _ platform.GroupManager = s
	var _ platform.InvitationHandler = s
	var _ platform.AutoModerator = s
	var _ platform.GroupInfoProvider = s
	var _ platform.AvatarProvider = s
	var _ platform.SessionNotifier = s
}
