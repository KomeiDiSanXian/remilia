package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepMerge(t *testing.T) {
	t.Run("scalar override", func(t *testing.T) {
		dst := map[string]any{"a": 1, "b": 2}
		src := map[string]any{"b": 3, "c": 4}
		result := deepMerge(dst, src)
		assert.Equal(t, 1, result["a"])
		assert.Equal(t, 3, result["b"])
		assert.Equal(t, 4, result["c"])
	})

	t.Run("nested merge", func(t *testing.T) {
		dst := map[string]any{"nested": map[string]any{"x": 1, "y": 2}}
		src := map[string]any{"nested": map[string]any{"y": 99, "z": 3}}
		result := deepMerge(dst, src)
		nested := result["nested"].(map[string]any)
		assert.Equal(t, 1, nested["x"])
		assert.Equal(t, 99, nested["y"])
		assert.Equal(t, 3, nested["z"])
	})

	t.Run("src is not a map, dst is", func(t *testing.T) {
		dst := map[string]any{"key": map[string]any{"a": 1}}
		src := map[string]any{"key": "scalar"}
		result := deepMerge(dst, src)
		assert.Equal(t, "scalar", result["key"])
	})

	t.Run("empty maps", func(t *testing.T) {
		result := deepMerge(map[string]any{}, map[string]any{})
		assert.Empty(t, result)
	})

	t.Run("dst nil", func(t *testing.T) {
		result := deepMerge(nil, map[string]any{"a": 1})
		assert.Equal(t, 1, result["a"])
	})
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"access_token", "accesstoken"},
		{"AccessToken", "accesstoken"},
		{"api_key", "apikey"},
		{"APIKey", "apikey"},
		{"Bot", "bot"},
		{"bot.qq.token", "bot.qq.token"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, normalizeKey(tt.input), "normalizeKey(%q)", tt.input)
	}
}

func TestLookupKey(t *testing.T) {
	m := map[string]any{
		"Bot":         1,
		"AccessToken": "secret",
		"APIKey":      "key123",
		"normal_key":  "value",
		"exact":       "match",
	}
	t.Run("exact match", func(t *testing.T) {
		k, ok := lookupKey(m, "exact")
		assert.True(t, ok)
		assert.Equal(t, "exact", k)
	})

	t.Run("normalized match", func(t *testing.T) {
		k, ok := lookupKey(m, "access_token")
		assert.True(t, ok)
		assert.Equal(t, "AccessToken", k)
	})

	t.Run("not found", func(t *testing.T) {
		_, ok := lookupKey(m, "nonexistent")
		assert.False(t, ok)
	})
}

func TestWalkGet(t *testing.T) {
	m := map[string]any{
		"bot": map[string]any{
			"qq": map[string]any{
				"token": "secret123",
			},
		},
	}
	t.Run("found", func(t *testing.T) {
		v := walkGet(m, []string{"bot", "qq", "token"})
		assert.Equal(t, "secret123", v)
	})

	t.Run("not found", func(t *testing.T) {
		v := walkGet(m, []string{"bot", "qq", "nonexistent"})
		assert.Nil(t, v)
	})

	t.Run("partial path", func(t *testing.T) {
		v := walkGet(m, []string{"bot", "qq"})
		assert.NotNil(t, v)
	})
}

func TestWalkSet(t *testing.T) {
	m := map[string]any{
		"bot": map[string]any{
			"token": "old",
		},
	}
	t.Run("sets value", func(t *testing.T) {
		walkSet(m, []string{"bot", "token"}, "new")
		assert.Equal(t, "new", walkGet(m, []string{"bot", "token"}))
	})

	t.Run("nonexistent path does nothing", func(t *testing.T) {
		walkSet(m, []string{"nonexistent", "key"}, "val")
	})

	t.Run("non-map intermediate does nothing", func(t *testing.T) {
		m2 := map[string]any{"key": "scalar"}
		walkSet(m2, []string{"key", "sub"}, "val")
	})
}

func TestDeleteConfigKey(t *testing.T) {
	m := map[string]any{
		"bot": map[string]any{
			"qq":  map[string]any{"token": "x"},
			"key": "keep",
		},
		"keep_me": "yes",
	}
	t.Run("delete nested", func(t *testing.T) {
		deleteConfigKey(m, "bot", "qq")
		_, ok := m["bot"].(map[string]any)["qq"]
		assert.False(t, ok)
	})

	t.Run("delete top-level", func(t *testing.T) {
		deleteConfigKey(m, "keep_me")
		_, ok := m["keep_me"]
		assert.False(t, ok)
	})

	t.Run("no-op with empty keys", func(t *testing.T) {
		deleteConfigKey(m)
	})

	t.Run("intermediate not a map", func(t *testing.T) {
		m2 := map[string]any{"key": "scalar"}
		deleteConfigKey(m2, "key", "sub")
	})
}

func TestMaskSensitive(t *testing.T) {
	input := map[string]any{
		"bot": map[string]any{
			"discord": map[string]any{"token": "abcdef123456"},
			"qq":      map[string]any{"token": "qqsecret!!"},
		},
		"plugins": map[string]any{
			"myplugin": map[string]any{"api_key": "mykey123"},
		},
	}
	result := maskSensitive(input)
	rm, ok := result.(map[string]any)
	require.True(t, ok)

	bot := rm["bot"].(map[string]any)
	discord := bot["discord"].(map[string]any)
	assert.Contains(t, discord["token"].(string), "****")
	assert.NotContains(t, discord["token"].(string), "abcdef123456")

	qq := bot["qq"].(map[string]any)
	assert.Contains(t, qq["token"].(string), "****")

	plugins := rm["plugins"].(map[string]any)
	mp := plugins["myplugin"].(map[string]any)
	assert.Contains(t, mp["api_key"].(string), "****")
}

func TestMaskSensitive_Empty(t *testing.T) {
	result := maskSensitive(map[string]any{})
	assert.NotNil(t, result)
}

func TestMaskSensitive_Nil(t *testing.T) {
	result := maskSensitive(nil)
	assert.Nil(t, result)
}

func TestMaskSensitive_ShortString(t *testing.T) {
	input := map[string]any{
		"bot": map[string]any{
			"discord": map[string]any{"token": "ab"},
		},
	}
	result := maskSensitive(input)
	rm := result.(map[string]any)
	token := rm["bot"].(map[string]any)["discord"].(map[string]any)["token"].(string)
	assert.Equal(t, "****", token)
}

func TestWriteOK(t *testing.T) {
	w := httptest.NewRecorder()
	writeOK(w, map[string]string{"key": "value"})
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp APIResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Code)
	assert.Equal(t, "ok", resp.Message)
}

func TestWriteErr(t *testing.T) {
	w := httptest.NewRecorder()
	writeErr(w, 404, "not found", 404)
	assert.Equal(t, 404, w.Code)

	var resp APIResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.Code)
	assert.Equal(t, "not found", resp.Message)
}

func TestWriteErr_InternalServerError(t *testing.T) {
	w := httptest.NewRecorder()
	writeErr(w, 500, "internal error", 500)
	assert.Equal(t, 500, w.Code)
}

func TestMaskTree(t *testing.T) {
	tree := map[string]any{
		"bot": map[string]any{
			"discord": map[string]any{"token": "mylongsecrettoken"},
		},
	}
	maskTree(tree)
	token := tree["bot"].(map[string]any)["discord"].(map[string]any)["token"].(string)
	assert.Contains(t, token, "my")
	assert.Contains(t, token, "en")
	assert.NotContains(t, token, "mylongsecrettoken")
}
