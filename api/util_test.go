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

func TestIsSensitiveKeyName(t *testing.T) {
	for _, key := range []string{
		"token", "Token", "api_key", "APIKey", "access_token", "AccessToken",
		"embedding_api_key", "EmbeddingAPIKey", "verify_token", "client_secret",
		"redis_password", "client_credential",
	} {
		assert.True(t, isSensitiveKeyName(key), "expected %q to be sensitive", key)
	}
	for _, key := range []string{
		"max_tokens", "max_tokens_cap", "tokenizer", "secret_santa", "format", "level",
	} {
		assert.False(t, isSensitiveKeyName(key), "expected %q not to be sensitive", key)
	}
}

// TestMaskSensitive_ExtendedCoverage 覆盖此前漏脱敏的字段：
// Telegram Token、api.api_key、插件嵌套配置与 embedding_api_key，
// 同时确认 max_tokens 之类的普通数值键不受影响。
func TestMaskSensitive_ExtendedCoverage(t *testing.T) {
	input := map[string]any{
		"API": map[string]any{"APIKey": "admin-key-1234"},
		"Bot": map[string]any{
			"Telegram": map[string]any{"Token": "123456:telegram-token"},
			"Milky":    map[string]any{"AccessToken": "milky-access-token"},
		},
		"Plugins": map[string]any{
			"Ai": map[string]any{
				"APIKey":          "sk-abcdefgh",
				"EmbeddingAPIKey": "emb-abcdefgh",
				"MaxTokens":       4096,
				"Providers": map[string]any{
					"Openai": map[string]any{"APIKey": "nested-provider-key"},
				},
			},
			"Knowledgebase": map[string]any{"EmbeddingAPIKey": "kb-key-1234"},
		},
	}

	result, ok := maskSensitive(input).(map[string]any)
	require.True(t, ok)

	assertMasked := func(t *testing.T, got any, original string) {
		t.Helper()
		s, ok := got.(string)
		require.True(t, ok, "expected string, got %T", got)
		assert.NotEqual(t, original, s)
		assert.Contains(t, s, "*")
	}
	assertPlain := func(t *testing.T, got any, want any) {
		t.Helper()
		assert.Equal(t, want, got)
	}

	apiSection := result["API"].(map[string]any)
	assertMasked(t, apiSection["APIKey"], "admin-key-1234")

	bot := result["Bot"].(map[string]any)
	assertMasked(t, bot["Telegram"].(map[string]any)["Token"], "123456:telegram-token")
	assertMasked(t, bot["Milky"].(map[string]any)["AccessToken"], "milky-access-token")

	plugins := result["Plugins"].(map[string]any)
	ai := plugins["Ai"].(map[string]any)
	assertMasked(t, ai["APIKey"], "sk-abcdefgh")
	assertMasked(t, ai["EmbeddingAPIKey"], "emb-abcdefgh")
	assertPlain(t, ai["MaxTokens"], float64(4096))
	assertMasked(t, ai["Providers"].(map[string]any)["Openai"].(map[string]any)["APIKey"], "nested-provider-key")
	assertMasked(t, plugins["Knowledgebase"].(map[string]any)["EmbeddingAPIKey"], "kb-key-1234")
}
