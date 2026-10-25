package admission

import (
	"context"
	"sync"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/middleware/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCtx(t *testing.T, cost eventctx.MatcherCost) *eventctx.Context {
	t.Helper()
	ctx := eventctx.NewContextFromEvent(&testutil.MiddlewareTestEvent{
		EventID:   "e1",
		EventKind: "GROUP_MESSAGE",
	}, nil)
	ctx.SetMatcherCost(cost)
	return ctx
}

// TestClassOf 覆盖「显式声明 > 观测 > 冷启动」的解析顺序。
func TestClassOf(t *testing.T) {
	a := New(Config{SlowP50: 500 * time.Millisecond, MinSamples: 3})

	tests := []struct {
		name string
		cost eventctx.MatcherCost
		want eventctx.CostClass
	}{
		{"declared io", eventctx.MatcherCost{Declared: eventctx.CostIO, P50: time.Millisecond, Samples: 10}, eventctx.CostIO},
		{"declared local beats observation", eventctx.MatcherCost{Declared: eventctx.CostLocal, P50: 10 * time.Second, Samples: 10}, eventctx.CostLocal},
		{"cold start is local", eventctx.MatcherCost{P50: 10 * time.Second, Samples: 1}, eventctx.CostLocal},
		{"observed slow is io", eventctx.MatcherCost{P50: 900 * time.Millisecond, Samples: 10}, eventctx.CostIO},
		{"observed fast is local", eventctx.MatcherCost{P50: 5 * time.Millisecond, Samples: 10}, eventctx.CostLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, a.classOf(newCtx(t, tt.cost)))
		})
	}
}

// TestLocalClassRejectsWhenFull 本地类不排队：槽位满立即拒绝并返回 ErrBusy。
func TestLocalClassRejectsWhenFull(t *testing.T) {
	entered := make(chan struct{})
	unblock := make(chan struct{})
	mw := New(Config{Local: ClassConfig{Limit: 1}})
	h := mw.Middleware()(func(*eventctx.Context) error {
		close(entered)
		<-unblock
		return nil
	})

	go func() { _ = h(newCtx(t, eventctx.MatcherCost{})) }()
	<-entered

	err := h(newCtx(t, eventctx.MatcherCost{}))
	assert.True(t, eventctx.IsBusy(err), "local class must reject immediately when full, got %v", err)

	close(unblock)
}

// TestIOClassQueuesThenAdmits I/O 类允许排队：槽位释放后等待者被放行。
func TestIOClassQueuesThenAdmits(t *testing.T) {
	var mu sync.Mutex
	running := 0
	maxRunning := 0
	entered := make(chan struct{}, 2)
	unblock := make(chan struct{})

	mw := New(Config{IO: ClassConfig{Limit: 1, Queue: 1, WaitTimeout: 2 * time.Second}})
	h := mw.Middleware()(func(*eventctx.Context) error {
		mu.Lock()
		running++
		if running > maxRunning {
			maxRunning = running
		}
		mu.Unlock()
		entered <- struct{}{}
		<-unblock
		mu.Lock()
		running--
		mu.Unlock()
		return nil
	})

	ioCost := eventctx.MatcherCost{Declared: eventctx.CostIO}
	firstDone := make(chan error, 1)
	go func() { firstDone <- h(newCtx(t, ioCost)) }()
	<-entered // 第一个占住唯一槽位

	secondDone := make(chan error, 1)
	go func() { secondDone <- h(newCtx(t, ioCost)) }()

	// 让第二个进入排队，然后释放第一个。
	time.Sleep(50 * time.Millisecond)
	unblock <- struct{}{}
	<-entered // 第二个被放行

	require.NoError(t, <-firstDone)
	unblock <- struct{}{}
	require.NoError(t, <-secondDone)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, maxRunning, "IO 类并发不得超过 Limit")
}

// TestIOQueueFullRejects 队满时拒绝（返回 ErrBusy），而不是无限排队。
func TestIOQueueFullRejects(t *testing.T) {
	entered := make(chan struct{}, 3)
	unblock := make(chan struct{})
	mw := New(Config{IO: ClassConfig{Limit: 1, Queue: 1, WaitTimeout: time.Second}})
	h := mw.Middleware()(func(*eventctx.Context) error {
		entered <- struct{}{}
		<-unblock
		return nil
	})

	ioCost := eventctx.MatcherCost{Declared: eventctx.CostIO}
	go func() { _ = h(newCtx(t, ioCost)) }()
	<-entered // 占用槽位
	go func() { _ = h(newCtx(t, ioCost)) }()
	time.Sleep(50 * time.Millisecond) // 占住唯一排队位

	err := h(newCtx(t, ioCost))
	assert.True(t, eventctx.IsBusy(err), "队满必须拒绝，got %v", err)

	close(unblock)
}

// TestNoLimitPassesThrough Limit<=0 表示该类不限制。
func TestNoLimitPassesThrough(t *testing.T) {
	mw := New(Config{})
	called := false
	h := mw.Middleware()(func(*eventctx.Context) error { called = true; return nil })
	require.NoError(t, h(newCtx(t, eventctx.MatcherCost{})))
	assert.True(t, called)
}

// TestQueueWaitRespectsContextCancel 排队等待必须响应 ctx 取消，避免停机时卡住。
func TestQueueWaitRespectsContextCancel(t *testing.T) {
	entered := make(chan struct{})
	unblock := make(chan struct{})
	mw := New(Config{IO: ClassConfig{Limit: 1, Queue: 1, WaitTimeout: time.Minute}})
	h := mw.Middleware()(func(*eventctx.Context) error { close(entered); <-unblock; return nil })

	ioCost := eventctx.MatcherCost{Declared: eventctx.CostIO}
	go func() { _ = h(newCtx(t, ioCost)) }()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newCtx(t, ioCost)
	c.SetStdContext(ctx)
	err := h(c)
	assert.True(t, eventctx.IsBusy(err))

	close(unblock)
}
