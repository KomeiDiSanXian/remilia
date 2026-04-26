package pluginctrl_test

import (
	stdctx "context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/pluginctrl"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/require"
)

func TestDebug_UnloadReregister(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background()) //nolint:errcheck
	pm := plugin.NewManager(eng)

	require.NoError(t, pm.Register(pluginctrl.New(pluginctrl.WithSuperUsers("su"))))
	ctrl := getPluginCtrl(t, pm)

	var handlerCalls int32
	makeDesc := func() *plugin.Descriptor {
		return &plugin.Descriptor{
			Name: "weather",
			Setup: func(ctx *plugin.SetupContext) (any, error) {
				m := ctx.Reg.RegisterMatcher(string(platform.EventKindGroupMessage))
				fmt.Printf("[DEBUG] weather Setup: registered matcher %p\n", m)
				m.Handle(func(_ *eventctx.Context) error {
					atomic.AddInt32(&handlerCalls, 1)
					fmt.Printf("[DEBUG] handler called! handlerCalls now = %d\n", atomic.LoadInt32(&handlerCalls))
					return nil
				})
				return nil, nil
			},
		}
	}

	// First registration
	require.NoError(t, pm.Register(makeDesc()))
	require.NoError(t, ctrl.SetGroupEnabled("g1", "weather", false))
	fmt.Println("[DEBUG] After first register + setGroupEnabled")

	eng.ProcessEvent(makeGroupCtx("g2", "u1"))
	fmt.Printf("[DEBUG] After g2 event (first round): handlerCalls=%d\n", atomic.LoadInt32(&handlerCalls))

	// Unregister
	require.NoError(t, pm.Unregister("weather"))
	fmt.Println("[DEBUG] After unregister")

	// Re-register
	atomic.StoreInt32(&handlerCalls, 0)
	require.NoError(t, pm.Register(makeDesc()))
	fmt.Println("[DEBUG] After second register")

	eng.ProcessEvent(makeGroupCtx("g2", "u1"))
	fmt.Printf("[DEBUG] After g2 event (second round): handlerCalls=%d\n", atomic.LoadInt32(&handlerCalls))
}
