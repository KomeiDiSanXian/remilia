package webimage

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// fakeRenderer returns a fixed payload and a nil error.
type fakeRenderer struct{ payload []byte }

func (f *fakeRenderer) Render(_ context.Context, _ string, _ bool, _ Options) ([]byte, error) {
	return f.payload, nil
}

// errRenderer always returns an error.
type errRenderer struct{ err error }

func (e *errRenderer) Render(_ context.Context, _ string, _ bool, _ Options) ([]byte, error) {
	return nil, e.err
}

// ─── New ──────────────────────────────────────────────────────────────────────

func TestNew_Defaults(t *testing.T) {
	c := New()
	assert.False(t, c.HasRenderer())
	assert.Equal(t, 1280, c.defaults.Width)
	assert.Equal(t, 720, c.defaults.Height)
	assert.Equal(t, 90, c.defaults.Quality)
	assert.Equal(t, FormatPNG, c.defaults.Format)
}

func TestNew_WithRenderer(t *testing.T) {
	payload := []byte("fake-png")
	c := New(WithRenderer(&fakeRenderer{payload: payload}))
	assert.True(t, c.HasRenderer())
}

func TestNew_WithDefaults(t *testing.T) {
	c := New(WithDefaults(Options{Width: 1920, Height: 1080, Quality: 75, Format: FormatJPEG}))
	assert.Equal(t, 1920, c.defaults.Width)
	assert.Equal(t, 1080, c.defaults.Height)
	assert.Equal(t, 75, c.defaults.Quality)
	assert.Equal(t, FormatJPEG, c.defaults.Format)
}

// ─── ErrNoRenderer ───────────────────────────────────────────────���────────────

func TestRender_NoRenderer_ReturnsErrNoRenderer(t *testing.T) {
	c := New()
	_, err := c.Render(context.Background(), "<h1>Hi</h1>")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoRenderer), "error should wrap ErrNoRenderer")
}

func TestRenderURL_NoRenderer_ReturnsErrNoRenderer(t *testing.T) {
	c := New()
	_, err := c.RenderURL(context.Background(), "https://example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoRenderer))
}

// ─── Render / RenderURL with renderer ────────────────────────────────────────

func TestRender_WithRenderer_ReturnsPayload(t *testing.T) {
	want := []byte{0x89, 0x50, 0x4E, 0x47} // fake "PNG" header
	c := New(WithRenderer(&fakeRenderer{payload: want}))

	got, err := c.Render(context.Background(), "<h1>Hello</h1>")
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

// ─── RendererFunc ─────────────────────────────────────────────────────────────

func TestRendererFunc_Adapter(t *testing.T) {
	called := false
	rf := RendererFunc(func(_ context.Context, src string, isURL bool, _ Options) ([]byte, error) {
		called = true
		assert.Equal(t, "html-content", src)
		assert.False(t, isURL)
		return []byte("result"), nil
	})
	c := New(WithRenderer(rf))

	got, err := c.Render(context.Background(), "html-content")
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, []byte("result"), got)
}

// ─── RenderOption overrides ───────────────────────────────────────────────────

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
	// Inject a no-op renderer so Render succeeds.
	c.SetRenderer(RendererFunc(func(_ context.Context, _ string, _ bool, _ Options) ([]byte, error) {
		return nil, nil
	}))

	_, _ = c.Render(context.Background(), "", WithWidth(4096))
	// Client defaults must be unmodified after the call.
	assert.Equal(t, 1280, c.defaults.Width, "defaults must not be mutated by per-call options")
}

// ─── isURL routing ────────────────────────────────────────────────────────────

func TestRenderVsRenderURL_IsURLFlag(t *testing.T) {
	var gotIsURL bool
	spy := RendererFunc(func(_ context.Context, _ string, isURL bool, _ Options) ([]byte, error) {
		gotIsURL = isURL
		return nil, nil
	})
	c := New(WithRenderer(spy))

	_, _ = c.Render(context.Background(), "<p/>")
	assert.False(t, gotIsURL, "Render must pass srcIsURL=false")

	_, _ = c.RenderURL(context.Background(), "https://example.com")
	assert.True(t, gotIsURL, "RenderURL must pass srcIsURL=true")
}
