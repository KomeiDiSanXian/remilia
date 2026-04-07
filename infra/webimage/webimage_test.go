package webimage

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 测试辅助 ──────────────────────────────────────────────────────────────────

// fakeRenderer 返回固定载荷，错误始终为 nil。
type fakeRenderer struct{ payload []byte }

func (f *fakeRenderer) Render(_ context.Context, _ string, _ bool, _ Options) ([]byte, error) {
	return f.payload, nil
}

// errRenderer 始终返回指定错误。
type errRenderer struct{ err error }

func (e *errRenderer) Render(_ context.Context, _ string, _ bool, _ Options) ([]byte, error) {
	return nil, e.err
}

// ─── New / 选项 ────────────────────────────────────────────────────────────────

func TestNew_Defaults(t *testing.T) {
	c := New()
	assert.False(t, c.HasRenderer())
	assert.Equal(t, 1280, c.defaults.Width)
	assert.Equal(t, 720, c.defaults.Height)
	assert.Equal(t, 90, c.defaults.Quality)
	assert.Equal(t, FormatPNG, c.defaults.Format)
}

func TestNew_WithRenderer(t *testing.T) {
	c := New(WithRenderer(&fakeRenderer{payload: []byte("fake-png")}))
	assert.True(t, c.HasRenderer())
}

func TestNew_WithDefaults(t *testing.T) {
	c := New(WithDefaults(Options{Width: 1920, Height: 1080, Quality: 75, Format: FormatJPEG}))
	assert.Equal(t, 1920, c.defaults.Width)
	assert.Equal(t, 1080, c.defaults.Height)
	assert.Equal(t, 75, c.defaults.Quality)
	assert.Equal(t, FormatJPEG, c.defaults.Format)
}

// ─── ErrNoRenderer ────────────────────────────────────────────────────────────

func TestRender_NoRenderer_ReturnsErrNoRenderer(t *testing.T) {
	c := New()
	_, err := c.Render(context.Background(), "<h1>你好</h1>")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoRenderer), "错误应包装 ErrNoRenderer")
}

func TestRenderURL_NoRenderer_ReturnsErrNoRenderer(t *testing.T) {
	c := New()
	_, err := c.RenderURL(context.Background(), "https://example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoRenderer))
}

// ─── 有渲染器时的 Render / RenderURL ──────────────────────────────────────────

func TestRender_WithRenderer_ReturnsPayload(t *testing.T) {
	want := []byte{0x89, 0x50, 0x4E, 0x47} // fake "PNG" header
	c := New(WithRenderer(&fakeRenderer{payload: want}))

	got, err := c.Render(context.Background(), "<h1>你好</h1>")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRenderURL_WithRenderer_ReturnsPayload(t *testing.T) {
	want := []byte("screenshot")
	c := New(WithRenderer(&fakeRenderer{payload: want}))

	got, err := c.RenderURL(context.Background(), "https://example.com")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestRender_RendererError_Propagates(t *testing.T) {
	sentinel := errors.New("browser crashed")
	c := New(WithRenderer(&errRenderer{err: sentinel}))

	_, err := c.Render(context.Background(), "<body/>")
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

// ─── SetRenderer ──────────────────────────────────────────────────────────────

func TestSetRenderer_AfterConstruction(t *testing.T) {
	c := New()
	assert.False(t, c.HasRenderer())

	c.SetRenderer(&fakeRenderer{payload: []byte("ok")})
	assert.True(t, c.HasRenderer())

	got, err := c.Render(context.Background(), "<p/>")
	require.NoError(t, err)
	assert.Equal(t, []byte("ok"), got)
}

// ─── RendererFunc 适配器 ───────────────────────────────────────────────────────

func TestRendererFunc_Adapter(t *testing.T) {
	called := false
	rf := RendererFunc(func(_ context.Context, src string, isURL bool, _ Options) ([]byte, error) {
		called = true
		assert.Equal(t, "html内容", src)
		assert.False(t, isURL)
		return []byte("result"), nil
	})
	c := New(WithRenderer(rf))

	got, err := c.Render(context.Background(), "html内容")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, []byte("result"), got)
}

// ─── RenderOption 覆盖验证 ────────────────────────────────────────────────────

func TestRenderOption_OverridesDefault(t *testing.T) {
	var capturedOpts Options
	spy := RendererFunc(func(_ context.Context, _ string, _ bool, o Options) ([]byte, error) {
		capturedOpts = o
		return nil, nil
	})
	c := New(
		WithRenderer(spy),
		WithDefaults(Options{Width: 1280, Height: 720, Quality: 90, Format: FormatPNG}),
	)

	_, _ = c.Render(context.Background(), "<h1/>",
		WithWidth(1920),
		WithHeight(1080),
		WithFormat(FormatJPEG),
		WithQuality(75),
		WithWaitSelector("#main"),
	)

	assert.Equal(t, 1920, capturedOpts.Width)
	assert.Equal(t, 1080, capturedOpts.Height)
	assert.Equal(t, FormatJPEG, capturedOpts.Format)
	assert.Equal(t, 75, capturedOpts.Quality)
	assert.Equal(t, "#main", capturedOpts.WaitSelector)
}

func TestRenderOption_DoesNotMutateDefaults(t *testing.T) {
	c := New(WithDefaults(Options{Width: 1280, Height: 720}))
	c.SetRenderer(RendererFunc(func(_ context.Context, _ string, _ bool, _ Options) ([]byte, error) {
		return nil, nil
	}))

	_, _ = c.Render(context.Background(), "", WithWidth(4096))
	// 调用完成后 Client 默认值不应被修改
	assert.Equal(t, 1280, c.defaults.Width, "单次覆盖选项不应修改 Client 默认值")
}

// ─── Render 与 RenderURL 的 isURL 路由 ────────────────────────────────────────

func TestRenderVsRenderURL_IsURLFlag(t *testing.T) {
	var gotIsURL bool
	spy := RendererFunc(func(_ context.Context, _ string, isURL bool, _ Options) ([]byte, error) {
		gotIsURL = isURL
		return nil, nil
	})
	c := New(WithRenderer(spy))

	_, _ = c.Render(context.Background(), "<p/>")
	assert.False(t, gotIsURL, "Render 应传递 srcIsURL=false")

	_, _ = c.RenderURL(context.Background(), "https://example.com")
	assert.True(t, gotIsURL, "RenderURL 应传递 srcIsURL=true")
}
