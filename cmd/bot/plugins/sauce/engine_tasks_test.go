package sauce

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngineTasksFiltersBySetAndEnable 验证任务列表按引擎集合与启用开关过滤。
func TestEngineTasksFiltersBySetAndEnable(t *testing.T) {
	p := &Plugin{cfg: &fakeConfig{vals: map[string]any{"saucenao_api_key": "key"}}}
	tasks := p.engineTasks(context.Background(), engineInput{}, 3, p.allEngines())
	names := map[string]bool{}
	for _, t := range tasks {
		names[t.name] = true
	}
	assert.True(t, names["SauceNAO"])
	assert.True(t, names["IQDB"])
	assert.True(t, names["TraceMoe"])
	assert.True(t, names["AnimeTrace"])

	// 仅指定部分引擎
	subset, err := p.parseEngineSet("tracemoe,animetrace")
	require.NoError(t, err)
	tasks = p.engineTasks(context.Background(), engineInput{}, 3, subset)
	require.Len(t, tasks, 2)
	assert.Equal(t, "TraceMoe", tasks[0].name)
	assert.Equal(t, "AnimeTrace", tasks[1].name)

	// 关闭 IQDB 配置后，即使 -engine 指定也不提交
	p2 := &Plugin{cfg: &fakeConfig{vals: map[string]any{"enable_iqdb": false}}}
	s, err := p2.parseEngineSet("iqdb,tracemoe")
	require.NoError(t, err)
	tasks = p2.engineTasks(context.Background(), engineInput{}, 3, s)
	require.Len(t, tasks, 1)
	assert.Equal(t, "TraceMoe", tasks[0].name)

	// 无 API key 时 SauceNAO 不提交
	p3 := &Plugin{}
	tasks = p3.engineTasks(context.Background(), engineInput{}, 3, p3.allEngines())
	for _, task := range tasks {
		assert.NotEqual(t, "SauceNAO", task.name)
	}
}

// TestSubmitEnginesCollectsAll 验证并发提交后能收集全部结果（含错误）。
func TestSubmitEnginesCollectsAll(t *testing.T) {
	tasks := []engineTask{
		{"A", func() ([]SearchResult, error) {
			return []SearchResult{{Title: "a1"}}, nil
		}},
		{"B", func() ([]SearchResult, error) {
			return nil, assertAnError{}
		}},
		{"C", func() ([]SearchResult, error) {
			return []SearchResult{{Title: "c1"}}, nil
		}},
	}
	ch := submitEngines(context.Background(), tasks)

	var names []string
	var errCount int
	for i := 0; i < len(tasks); i++ {
		res := <-ch
		names = append(names, res.name)
		if res.err != nil {
			errCount++
		}
	}
	assert.Len(t, names, 3)
	assert.Equal(t, 1, errCount)
}

type assertAnError struct{}

func (assertAnError) Error() string { return "boom" }
