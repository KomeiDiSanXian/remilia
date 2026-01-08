package remilia

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestCommandDispatchOptimization(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	var calledPing int32
	var calledHelp int32
	var calledGeneric int32

	// 1. Register optimized command matchers
	// Use new OnCommand helper
	e.OnCommand(dto.C2CMessageCreate, "/ping", func(ctx *Context) bool {
		return true
	}).Handle(func(ctx *Context) {
		atomic.AddInt32(&calledPing, 1)
	})

	// Use manual BindCommand for normal matcher optimization
	e.On(dto.C2CMessageCreate, OnCommand("/help")).BindCommand("/help").Handle(func(ctx *Context) {
		atomic.AddInt32(&calledHelp, 1)
	})

	// 2. Register normal matcher (no command optimization)
	// Priority 10 (higher than default 50)
	e.On(dto.C2CMessageCreate, func(ctx *Context) bool {
		return true
	}).SetPriority(10).Handle(func(ctx *Context) {
		atomic.AddInt32(&calledGeneric, 1)
	})

	// 3. Test "/ping"
	ctxPing := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"/ping"}`),
	}, nil)
	e.ProcessEvent(ctxPing)
	// Should call Generic (p=10) then Ping (p=50)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledGeneric), "Generic should be called")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledPing), "Ping should be called")
	assert.Equal(t, int32(0), atomic.LoadInt32(&calledHelp), "Help should NOT be called")

	// 4. Test "/help argument"
	ctxHelp := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"/help me"}`),
	}, nil)
	e.ProcessEvent(ctxHelp)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calledGeneric), "Generic should be called again")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledPing), "Ping should NOT be called again")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledHelp), "Help should be called")

	// 5. Test unknown command "/foo"
	// Should only call Generic
	ctxFoo := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"/foo"}`),
	}, nil)
	e.ProcessEvent(ctxFoo)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calledGeneric), "Generic should be called 3rd time")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledPing), "Ping should stay same")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledHelp), "Help should stay same")

	// 6. Test partial match "/pingpong"
	// With Hash Map optimization, this should NOT match "/ping"
	ctxPingPong := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"/pingpong"}`),
	}, nil)
	e.ProcessEvent(ctxPingPong)
	assert.Equal(t, int32(4), atomic.LoadInt32(&calledGeneric))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledPing), "Ping should NOT match /pingpong with BindCommand")
}

func TestCommandDispatchPriorityMixing(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	var executionOrder []string

	// P=10: Generic High
	e.OnC2C(func(ctx *Context) bool { return true }).SetPriority(10).Handle(func(ctx *Context) {
		executionOrder = append(executionOrder, "GenHigh")
	})

	// P=20: Command High "/test"
	e.OnCommand(dto.C2CMessageCreate, "/test").SetPriority(20).Handle(func(ctx *Context) {
		executionOrder = append(executionOrder, "CmdHigh")
	})

	// P=30: Generic Mid
	e.OnC2C(func(ctx *Context) bool { return true }).SetPriority(30).Handle(func(ctx *Context) {
		executionOrder = append(executionOrder, "GenMid")
	})

	// P=40: Command Low "/test"
	e.OnCommand(dto.C2CMessageCreate, "/test").SetPriority(40).Handle(func(ctx *Context) {
		executionOrder = append(executionOrder, "CmdLow")
	})

	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"/test args"}`),
	}, nil)
	e.ProcessEvent(ctx)

	expected := []string{"GenHigh", "CmdHigh", "GenMid", "CmdLow"}
	assert.Equal(t, expected, executionOrder)
}

func TestCommandDispatchConcurrency(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	// Register 100 command matchers
	for i := 0; i < 100; i++ {
		cmd := "/cmd" // same command
		e.OnCommand(dto.C2CMessageCreate, cmd).Handle(func(ctx *Context) {
			// heavier work
			time.Sleep(time.Microsecond)
		})
	}

	// Benchmark style test
	start := time.Now()
	ctx := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"/cmd"}`),
	}, nil)
	e.ProcessEvent(ctx)
	duration := time.Since(start)
	t.Logf("Processed 100 matchers in %v", duration)
}

func TestFullMatchOptimization(t *testing.T) {
	e := NewEngine()
	defer e.Close()

	var calledFull int32
	var calledWrong int32

	// Optimize OnFullMatch("hello world") using BindCommand("hello")
	// This makes engine use Hash lookup for "hello" (first word), then run exact match rule.
	e.OnAny(OnFullMatch("hello world")).BindCommand("hello").Handle(func(ctx *Context) {
		atomic.AddInt32(&calledFull, 1)
	})

	// Matcher strictly for "hello" to verify no conflict
	e.OnAny(OnFullMatch("hello")).BindCommand("hello").Handle(func(ctx *Context) {
		atomic.AddInt32(&calledWrong, 1)
	})

	// 1. Valid Input
	ctx1 := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"hello world"}`),
	}, nil)
	e.ProcessEvent(ctx1)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledFull), "Should match full string")
	assert.Equal(t, int32(0), atomic.LoadInt32(&calledWrong), "Should NOT match wrong string")

	// 2. Partial Input (matches index, fails rule)
	ctx2 := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"hello universe"}`),
	}, nil)
	e.ProcessEvent(ctx2)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledFull), "Should stay 1")
	assert.Equal(t, int32(0), atomic.LoadInt32(&calledWrong), "Should stay 0")

	// 3. Exact first word (matches index, hits second matcher)
	ctx3 := NewContext(&dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: json.RawMessage(`{"content":"hello"}`),
	}, nil)
	e.ProcessEvent(ctx3)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledFull), "Should stay 1")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calledWrong), "Should match exact hello")
}
