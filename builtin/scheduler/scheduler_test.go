package scheduler_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/builtin/scheduler"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

// newSched 返回已完成 Setup 的 Plugin 和清理函数。
// 使用 NewPlugin()+p.Descriptor() 模式，在注册前持有引用。
func newSched(t *testing.T) (*scheduler.Plugin, func()) {
	t.Helper()
	p := scheduler.NewPlugin()
	desc := p.Descriptor()
	pm := plugin.NewManager(engine.NewEngine())
	if err := pm.Register(desc); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return p, func() {
		if err := pm.Unregister(context.Background(), "scheduler"); err != nil {
			t.Logf("Unregister: %v", err)
		}
	}
}

func TestScheduler_Every(t *testing.T) {
	p, stop := newSched(t)
	defer stop()
	var count atomic.Int32
	id := p.Every(20*time.Millisecond, func() { count.Add(1) })
	if id == 0 {
		t.Fatal("expected non-zero job ID")
	}
	time.Sleep(120 * time.Millisecond)
	if count.Load() < 2 {
		t.Errorf("expected >= 2 executions, got %d", count.Load())
	}
}
func TestScheduler_Cron(t *testing.T) {
	p, stop := newSched(t)
	defer stop()
	var fired atomic.Bool
	id := p.Cron("* * * * * *", func() { fired.Store(true) })
	if id == 0 {
		t.Fatal("expected non-zero job ID")
	}
	time.Sleep(1500 * time.Millisecond)
	if !fired.Load() {
		t.Error("cron job should have fired")
	}
}
func TestScheduler_Remove(t *testing.T) {
	p, stop := newSched(t)
	defer stop()
	var count atomic.Int32
	id := p.Every(10*time.Millisecond, func() { count.Add(1) })
	time.Sleep(30 * time.Millisecond)
	p.Remove(id)
	before := count.Load()
	time.Sleep(40 * time.Millisecond)
	if after := count.Load(); after > before+1 {
		t.Errorf("job still running after Remove: before=%d after=%d", before, after)
	}
}
func TestScheduler_Jobs(t *testing.T) {
	p, stop := newSched(t)
	defer stop()
	if p.Jobs() != 0 {
		t.Error("expected 0 jobs initially")
	}
	id1 := p.Every(1*time.Hour, func() {})
	id2 := p.Every(2*time.Hour, func() {})
	if p.Jobs() != 2 {
		t.Errorf("expected 2 jobs, got %d", p.Jobs())
	}
	p.Remove(id1)
	p.Remove(id2)
	if p.Jobs() != 0 {
		t.Errorf("expected 0 after removal, got %d", p.Jobs())
	}
}
func TestScheduler_PanicRecovery(t *testing.T) {
	p, stop := newSched(t)
	defer stop()
	var after atomic.Bool
	p.Every(10*time.Millisecond, func() { panic("test panic") })
	p.Every(50*time.Millisecond, func() { after.Store(true) })
	time.Sleep(120 * time.Millisecond)
	if !after.Load() {
		t.Error("scheduler should continue after panic in one job")
	}
}

// noopLogger satisfies plugin.Logger for tests without panicking on nil.
type noopLogger struct{}

func (noopLogger) Info(_ string)                           {}
func (noopLogger) Infof(_ string, _ ...any)                {}
func (noopLogger) Infow(_ string, _ ...any)                {}
func (noopLogger) Warn(_ string)                           {}
func (noopLogger) Warnf(_ string, _ ...any)                {}
func (noopLogger) Warnw(_ string, _ ...any)                {}
func (noopLogger) Error(_ string, _ error)                 {}
func (noopLogger) Errorf(_ string, _ ...any)               {}
func (noopLogger) Debug(_ string)                          {}
func (noopLogger) Debugf(_ string, _ ...any)               {}
func (noopLogger) Debugw(_ string, _ ...any)               {}
func (noopLogger) WithField(_ string, _ any) plugin.Logger { return noopLogger{} }
