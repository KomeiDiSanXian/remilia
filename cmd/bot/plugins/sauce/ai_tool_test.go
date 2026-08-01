package sauce

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTools(t *testing.T) {
	p := &Plugin{}
	tools := p.ListTools()
	require.Len(t, tools, 1)

	tool := tools[0]
	assert.Equal(t, "sauce", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.NotNil(t, tool.Execute)
	assert.Contains(t, tool.Parameters.Required, "image_url")
	props := tool.Parameters.Properties
	assert.NotNil(t, props["image_url"])
	assert.NotNil(t, props["db"])
	assert.NotNil(t, props["max_results"])
}

func TestExecuteSauceToolMissingImageURL(t *testing.T) {
	p := &Plugin{}
	_, err := p.executeSauceTool(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_url")

	_, err = p.executeSauceTool(context.Background(), map[string]any{"image_url": "   "})
	require.Error(t, err)
}

func TestExecuteSauceToolBadURL(t *testing.T) {
	p := &Plugin{}
	_, err := p.executeSauceTool(context.Background(), map[string]any{"image_url": "http://127.0.0.1:1/x.png"})
	require.Error(t, err)
}

func TestExecuteSauceToolCustomDBWithoutKey(t *testing.T) {
	p := &Plugin{}
	_, err := p.executeSauceTool(context.Background(), map[string]any{"image_url": "http://example.com/x.png", "db": float64(5)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API Key")
}

func TestFirstStringArg(t *testing.T) {
	assert.Equal(t, "https://example.com/a.png", firstStringArg("https://example.com/a.png"))
	assert.Equal(t, "42", firstStringArg(float64(42)))
	assert.Equal(t, "", firstStringArg(nil))
	assert.Equal(t, "", firstStringArg(123))
}

func TestFinishToolSearchEmpty(t *testing.T) {
	p := &Plugin{}
	out, err := p.finishToolSearch(nil, nil, 3)
	require.NoError(t, err)
	assert.Equal(t, "未找到匹配结果", out)
}
