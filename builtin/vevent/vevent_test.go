package vevent_test

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/vevent"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// fakeProcessor 记录注入的事件，用于测试。
type fakeProcessor struct {
	events []platform.Event
	sender platform.Sender
}

func (f *fakeProcessor) ProcessPlatformEvent(event platform.Event, sender platform.Sender, _ ...platform.Capabilities) {
	f.events = append(f.events, event)
	f.sender = sender
}

func (f *fakeProcessor) ProcessPlatformEventSync(event platform.Event, sender platform.Sender, _ ...platform.Capabilities) {
	f.events = append(f.events, event)
	f.sender = sender
}

func TestPlugin_Inject(t *testing.T) {
	proc := &fakeProcessor{}
	p := vevent.NewPlugin(proc)

	p.Inject(platform.EventKindGroupMessage, "/ping",
		vevent.WithChat(platform.ChatInfo{ID: "g1", IsGroup: true}),
		vevent.WithSender(platform.UserInfo{ID: "u1"}),
	)

	if len(proc.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(proc.events))
	}
	evt := proc.events[0]
	if evt.Kind() != platform.EventKindGroupMessage {
		t.Errorf("expected kind %s, got %s", platform.EventKindGroupMessage, evt.Kind())
	}
	if evt.Content() != "/ping" {
		t.Errorf("expected content /ping, got %q", evt.Content())
	}
	if evt.Chat().ID != "g1" {
		t.Errorf("expected chat ID g1, got %q", evt.Chat().ID)
	}
	if evt.Sender().ID != "u1" {
		t.Errorf("expected sender ID u1, got %q", evt.Sender().ID)
	}
	// NoopSender 应作为默认 sender
	if proc.sender == nil {
		t.Error("sender should not be nil")
	}
}

func TestPlugin_InjectWithSender(t *testing.T) {
	proc := &fakeProcessor{}
	p := vevent.NewPlugin(proc)

	customSender := &platform.NoopSender{}
	p.InjectWithSender(platform.EventKindPrivateMessage, "hello", customSender,
		vevent.WithSender(platform.UserInfo{ID: "u2"}),
	)

	if len(proc.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(proc.events))
	}
	if proc.sender != customSender {
		t.Error("expected custom sender to be used")
	}
}

func TestPlugin_InjectEvent(t *testing.T) {
	proc := &fakeProcessor{}
	p := vevent.NewPlugin(proc)

	evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "/hello",
		platform.WithSyntheticID("test-id-001"),
		platform.WithSyntheticPlatform("test"),
	)
	p.InjectEvent(evt, &platform.NoopSender{})

	if len(proc.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(proc.events))
	}
	if proc.events[0].ID() != "test-id-001" {
		t.Errorf("expected ID test-id-001, got %q", proc.events[0].ID())
	}
}

func TestSyntheticEvent_Interface(t *testing.T) {
	// 编译期断言：*SyntheticEvent 实现了 platform.Event
	var _ platform.Event = (*platform.SyntheticEvent)(nil)
}
