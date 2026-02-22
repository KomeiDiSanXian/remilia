package scheduler_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/KomeiDiSanXian/remilia/plugins/scheduler"
)

func newSchedulerPlugin(t *testing.T) (*scheduler.Plugin, func()) {
	t.Helper()
	p, desc := scheduler.NewPlugin()
	eng := engine.NewEngine()
	pm := plugin.NewManager(eng)
	if err := pm.RegisterV2(desc); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	stop := func() { desc.Teardown() }
	return p, stop
}
func TestScheduler_Every(t *testing.T) {
	p, stop := newSchedulerPlugin(t)
	defer stop()
	var count atomic.Int32
	id := p.Every(20*time.Millisecond, func() { count.Add(1) })
	if id == 0 {
		t.Fatal("expected non-zero job ID")
	}
	time.Sleep(120 * time.Millisecond)
	if count.Load() < 2 {
		t.Errorf("expected at least 2 executions, got %d", count.Load())
	}
}
func TestScheduler_Cron(t *testing.T) {
	p, stop := newSchedulerPlugin(t)
	defer stop()
	var fired atomic.Bool
	id := p.Cron("* * * * * *", func() { fired.Store(true) })
	if id == 0 {
		t.Fatal("expected non-zero job ID")
	}
	time.Sleep(1500 * time.Millisecond)
	if !fired.Load() {
		t.Error("cron job should have fired at least once")
	}
}
func TestScheduler_Remove(t *testing.T) {
	p, stop := newSchedulerPlugin(t)
	defer stop()
	var count atomic.Int32
	id := p.Every(10*time.Millisecond, func() { count.Add(1) })
	time.Sleep(30 * time.Millisecond)
	p.Remove(id)
	before := count.Load()
	time.Sleep(40 * time.Millisecond)
	after := count.Load()
	if after > before+1 {
		t.Errorf("job should have been removed: before=%d after=%d", before, after)
	}
}
func TestScheduler_Jobs(t *testing.T) {
	p, stop := newSchedulerPlugin(t)
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
		t.Errorf("expected 0 jobs after removal, got %d", p.Jobs())
	}
}
func TestScheduler_PanicRecovery(t *testing.T) {
	p, stop := newSchedulerPlugin(t)
	defer stop()
	var after atomic.Bool
	p.Every(10*time.Millisecond, func() { panic("test panic") })
	p.Every(50*time.Millisecond, func() { after.Store(true) })
	time.Sleep(120 * time.Millisecond)
	if !after.Load() {
		t.Error("scheduler should continue after panic in one job")
	}
}
