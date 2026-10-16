package api

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginStatusToDetail(t *testing.T) {
	loadTime := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	status := &plugin.Status{
		Name:                  "demo",
		State:                 plugin.Loaded,
		LoadTime:              loadTime,
		LastError:             errors.New("boom"),
		MatcherCount:          3,
		Uptime:                90 * time.Second,
		HasSaveState:          true,
		EventBusSubscriptions: 2,
		GoroutineCount:        1,
		Metadata:              &plugin.Metadata{Name: "demo", Version: "1.0.0"},
	}

	detail := pluginStatusToDetail(status)
	assert.Equal(t, "demo", detail.Name)
	assert.Equal(t, "Loaded", detail.State)
	assert.Equal(t, "1m30s", detail.Uptime)
	assert.Equal(t, 3, detail.MatcherCount)
	assert.Equal(t, "boom", detail.LastError)
	assert.Equal(t, loadTime.Format(time.RFC3339), detail.LoadTime)
	assert.True(t, detail.HasSaveState)
	assert.Equal(t, 2, detail.EventBusSubscriptions)
	assert.Equal(t, 1, detail.GoroutineCount)
	require.NotNil(t, detail.Metadata)
	assert.Equal(t, "demo", detail.Metadata.Name)
}

func TestPluginStatusToDetail_NoError(t *testing.T) {
	detail := pluginStatusToDetail(&plugin.Status{Name: "demo", State: plugin.Disabled})
	assert.Equal(t, "Disabled", detail.State)
	assert.Empty(t, detail.LastError)
	assert.Empty(t, detail.LoadTime)
}

// TestPluginDetailJSONKeys 锁定详情响应的线上字段名：
// 前端按 snake_case 读取，不能再把 State 序列化成数字或 LastError 序列化成对象。
func TestPluginDetailJSONKeys(t *testing.T) {
	detail := pluginStatusToDetail(&plugin.Status{
		Name:         "demo",
		State:        plugin.Error,
		LastError:    errors.New("kaboom"),
		Uptime:       5 * time.Second,
		MatcherCount: 2,
	})

	data, err := json.Marshal(detail)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "Error", m["state"])
	assert.Equal(t, "5s", m["uptime"])
	assert.Equal(t, "kaboom", m["last_error"])
	assert.Equal(t, float64(2), m["matcher_count"])
	assert.Equal(t, false, m["has_save_state"])
	assert.NotContains(t, m, "State")
	assert.NotContains(t, m, "LastError")
}
