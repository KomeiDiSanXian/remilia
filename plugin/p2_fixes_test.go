package plugin

import (
	stdctx "context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestConfig_Override_PersistsAfterReload(t *testing.T) {
	cfg := NewPluginConfig("testplugin", nil)
	require.NoError(t, cfg.Override("key", "overridden-value"))
	require.NoError(t, cfg.Reload())
	assert.Equal(t, "overridden-value", cfg.GetString("key", "default"),
		"Override 值在 Reload 后应当持久保留")
}
func TestConfig_Override_MultipleKeys_AllPersist(t *testing.T) {
	cfg := NewPluginConfig("testplugin", nil)
	require.NoError(t, cfg.Override("a", 42))
	require.NoError(t, cfg.Override("b", true))
	require.NoError(t, cfg.Override("c", "hello"))
	require.NoError(t, cfg.Reload())
	assert.Equal(t, 42, cfg.GetInt("a", 0))
	assert.Equal(t, true, cfg.GetBool("b", false))
	assert.Equal(t, "hello", cfg.GetString("c", ""))
}
func TestConfig_Override_OnChangeStillFires(t *testing.T) {
	cfg := NewPluginConfig("testplugin", nil)
	var gotKey string
	var gotNew any
	cfg.OnChange(func(key string, oldVal, newVal any) { gotKey = key; gotNew = newVal })
	require.NoError(t, cfg.Override("mykey", "myval"))
	assert.Equal(t, "mykey", gotKey)
	assert.Equal(t, "myval", gotNew)
}
func TestPluginInstance_GetAPI(t *testing.T) {
	pm := NewManager(nil)
	type myAPI struct{ Value string }
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name:  "get-api-test",
		Setup: func(ctx *SetupContext) (any, error) { return &myAPI{Value: "exported"}, nil },
	}))
	inst, ok := pm.Get("get-api-test")
	require.True(t, ok)
	api := inst.GetAPI()
	require.NotNil(t, api)
	typed, ok := api.(*myAPI)
	require.True(t, ok)
	assert.Equal(t, "exported", typed.Value)
}
func TestPluginInstance_GetAPI_NilWhenNotExported(t *testing.T) {
	pm := NewManager(nil)
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name:  "nil-api-plugin",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}))
	inst, ok := pm.Get("nil-api-plugin")
	require.True(t, ok)
	assert.Nil(t, inst.GetAPI())
}
func TestStatus_GoroutineCount(t *testing.T) {
	pm := NewManager(nil)
	ready := make(chan struct{})
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name: "goroutine-count-test",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.GoNamed("worker-1", func(runCtx stdctx.Context) {
				close(ready)
				<-runCtx.Done()
			})
			return nil, nil
		},
	}))
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not start in time")
	}
	status, err := pm.GetStatus("goroutine-count-test")
	require.NoError(t, err)
	assert.Equal(t, 1, status.GoroutineCount, "Status 应反映活跃 goroutine 数量")
}
func TestStatus_GoroutineCount_ZeroWhenNoGoroutines(t *testing.T) {
	pm := NewManager(nil)
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name:  "no-goroutine-plugin",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}))
	status, err := pm.GetStatus("no-goroutine-plugin")
	require.NoError(t, err)
	assert.Equal(t, 0, status.GoroutineCount)
}
func TestTeardownContext_InfoField(t *testing.T) {
	pm := NewManager(nil)
	var teardownInfoNotNil bool
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name:  "teardown-info-provider",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
	}))
	require.NoError(t, pm.RegisterV2(&PluginDescriptor{
		Name:  "teardown-info-consumer",
		Setup: func(ctx *SetupContext) (any, error) { return nil, nil },
		Teardown: func(ctx *TeardownContext) error {
			teardownInfoNotNil = ctx.Info != nil
			return nil
		},
	}))
	require.NoError(t, pm.Unregister("teardown-info-consumer"))
	assert.True(t, teardownInfoNotNil, "TeardownContext.Info 应为非 nil")
}
func TestPluginLogger_Infow_DoesNotPanic(t *testing.T) {
	log := newPluginLogger("test-plugin")
	assert.NotPanics(t, func() { log.Infow("user action", "userID", "u123", "action", "login") })
	assert.NotPanics(t, func() { log.Warnw("suspicious activity", "ip", "1.2.3.4", "count", 42) })
	assert.NotPanics(t, func() { log.Debugw("trace", "key", "val") })
}
func TestPluginLogger_Infow_OddKVsHandledGracefully(t *testing.T) {
	log := newPluginLogger("test-plugin")
	assert.NotPanics(t, func() { log.Infow("msg", "key1", "val1", "orphan-key") })
}
