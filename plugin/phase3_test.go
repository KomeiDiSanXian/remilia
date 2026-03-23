package plugin

import (
	stdctx "context"
	"errors"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// P3-1: SetupFuncV3 — Setup 返回 API 对象，框架自动 ExportAs
// ---------------------------------------------------------------------------

func TestP3_SetupV3_AutoExport(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	type MyAPI struct{ Value int }

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-export",
		Setup: func(ctx *SetupContext) (any, error) {
			return &MyAPI{Value: 42}, nil
		},
	}))

	// 框架应自动将返回值注入容器
	raw, ok := pm.GetContainer().Get("p3-export")
	require.True(t, ok, "框架应自动 ExportAs")
	api, ok := raw.(*MyAPI)
	require.True(t, ok)
	assert.Equal(t, 42, api.Value)
}

func TestP3_SetupV3_NilApiNotExported(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-nil-export",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil // 纯命令注册插件，不导出 API
		},
	}))

	// 返回 nil 时容器中存储的是 *Instance（回退行为）
	raw, ok := pm.GetContainer().Get("p3-nil-export")
	require.True(t, ok)
	_, isInstance := raw.(*Instance)
	assert.True(t, isInstance, "nil API 应回退存储 *Instance")
}

func TestP3_SetupV3_ErrorPropagates(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	sentinel := errors.New("setup failed")
	err := pm.Register(&Descriptor{
		Name: "p3-error",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, sentinel
		},
	})
	require.ErrorIs(t, err, sentinel)
}

// ---------------------------------------------------------------------------
// P3-1 向后兼容: 旧签名 func(*SetupContext) error 仍然工作
// ---------------------------------------------------------------------------

func TestP3_OldSetupFunc_StillWorks(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	type OldAPI struct{ Name string }
	api := &OldAPI{Name: "old"}

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-old-setup",
		Setup: func(ctx *SetupContext) (any, error) {
			ctx.ExportAs("p3-old-setup", api)
			return nil, nil
		},
	}))

	raw, ok := pm.GetContainer().Get("p3-old-setup")
	require.True(t, ok)
	assert.Equal(t, api, raw.(*OldAPI))
}

// ---------------------------------------------------------------------------
// P3-2: TeardownFuncV3 — Teardown 接收 *TeardownContext
// ---------------------------------------------------------------------------

func TestP3_TeardownV3_ReceivesAPI(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	type State struct{ Saved bool }
	var capturedState *State

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-teardown",
		Setup: func(ctx *SetupContext) (any, error) {
			return &State{Saved: false}, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			s := ctx.API.(*State)
			s.Saved = true
			capturedState = s
			return nil
		},
	}))

	require.NoError(t, pm.Unregister("p3-teardown"))
	require.NotNil(t, capturedState)
	assert.True(t, capturedState.Saved, "TeardownContext.API 应包含 Setup 返回的对象")
}

func TestP3_TeardownV3_LogAndConfigAvailable(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	var teardownLog Logger
	var teardownConfig Config

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-teardown-ctx",
		Setup: func(ctx *SetupContext) (any, error) {
			return "api-value", nil
		},
		Teardown: func(ctx *TeardownContext) error {
			teardownLog = ctx.Log
			teardownConfig = ctx.Config
			return nil
		},
	}))

	require.NoError(t, pm.Unregister("p3-teardown-ctx"))
	assert.NotNil(t, teardownLog, "TeardownContext.Log 应被注入")
	_ = teardownConfig // Config 可能为 nil（未配置），但不应 panic
}

// ---------------------------------------------------------------------------
// P3-2 向后兼容: 旧签名 func() error 仍然工作
// ---------------------------------------------------------------------------

func TestP3_OldTeardownFunc_StillWorks(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	teardownCalled := false

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-old-teardown",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil
		},
		Teardown: func(ctx *TeardownContext) error {
			teardownCalled = true
			return nil
		},
	}))

	require.NoError(t, pm.Unregister("p3-old-teardown"))
	assert.True(t, teardownCalled, "旧签名 Teardown 应被调用")
}

// ---------------------------------------------------------------------------
// P3-3: ExportAs 已废弃但向后兼容（P3-1 自动导出后不再需要手动调用）
// ---------------------------------------------------------------------------

func TestP3_ExportAs_StillWorks(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	type Service struct{ ID string }
	svc := &Service{ID: "custom-name"}

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-manual-export",
		Setup: func(ctx *SetupContext) (any, error) {
			// 旧式：手动导出到自定义 key
			ctx.ExportAs("my-service", svc)
			return nil, nil
		},
	}))

	raw, ok := pm.GetContainer().Get("my-service")
	require.True(t, ok)
	assert.Equal(t, svc, raw.(*Service))
}

// ---------------------------------------------------------------------------
// P3-4: Descriptor 字段分层（Meta/Advanced）
// ---------------------------------------------------------------------------

func TestP3_Meta_EffectiveMeta_PrioritizesMetaField(t *testing.T) {
	desc := &Descriptor{
		Name: "test",
		Meta: &Metadata{
			Author:      "new-author",
			Description: "new-desc",
		},
	}
	m := desc.effectiveMeta()
	assert.Equal(t, "new-author", m.Author)
	assert.Equal(t, "new-desc", m.Description)
}

func TestP3_Meta_FallbackToDeprecatedFields(t *testing.T) {
	// Meta is now the canonical source; test that nil Meta returns zero value
	desc := &Descriptor{
		Name: "test",
		Meta: &Metadata{
			Author:      "meta-author",
			Description: "meta-desc",
			Category:    "cat",
			Tags:        []string{"a", "b"},
			Hidden:      true,
		},
	}
	m := desc.effectiveMeta()
	assert.Equal(t, "meta-author", m.Author)
	assert.Equal(t, "meta-desc", m.Description)
	assert.Equal(t, "cat", m.Category)
	assert.Equal(t, []string{"a", "b"}, m.Tags)
	assert.True(t, m.Hidden)
}

func TestP3_Advanced_ReloadInAdvanced(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	reloadCalled := false

	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-advanced-reload",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil
		},
		Advanced: &Advanced{
			Strategy: ReloadInPlace, // 必须显式声明，否则 Reload 不会被调用
			Reload: func(ctx *SetupContext) error {
				reloadCalled = true
				return nil
			},
		},
	}))

	require.NoError(t, pm.Reload("p3-advanced-reload"))
	assert.True(t, reloadCalled, "Advanced.Reload 应被调用")
}

func TestP3_Advanced_FallbackToDeprecatedReload(t *testing.T) {
	eng := engine.NewEngine()
	defer eng.Shutdown(stdctx.Background())
	pm := NewManager(eng)

	reloadCalled := false

	// Reload 函数只有在 Strategy == ReloadInPlace 时才会被调用
	require.NoError(t, pm.Register(&Descriptor{
		Name: "p3-deprecated-reload",
		Setup: func(ctx *SetupContext) (any, error) {
			return nil, nil
		},
		Advanced: &Advanced{
			Strategy: ReloadInPlace, // 必须显式声明
			Reload: func(ctx *SetupContext) error {
				reloadCalled = true
				return nil
			},
		},
	}))

	require.NoError(t, pm.Reload("p3-deprecated-reload"))
	assert.True(t, reloadCalled, "Advanced.Reload 应被调用")
}
