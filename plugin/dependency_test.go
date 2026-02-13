package plugin

import (
	"reflect"
	"testing"

	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 测试用的插件结构
type testPlugin struct {
	*BasePlugin
	Dep1 *testDep1 `inject:"plugin:dep1"`
	Dep2 *testDep2 `inject:"plugin:dep2,required"`
	Dep3 *testDep3 // 没有标签，不应该被提取
}

type testDep1 struct {
	*BasePlugin
}

type testDep2 struct {
	*BasePlugin
}

type testDep3 struct {
	*BasePlugin
}

func (p *testPlugin) Load(_ *engine.Engine) error   { return nil }
func (p *testPlugin) Unload(_ *engine.Engine) error { return nil }
func (p *testPlugin) Reload(_ *engine.Engine) error { return nil }

func (p *testDep1) Load(_ *engine.Engine) error   { return nil }
func (p *testDep1) Unload(_ *engine.Engine) error { return nil }
func (p *testDep1) Reload(_ *engine.Engine) error { return nil }

func (p *testDep2) Load(_ *engine.Engine) error   { return nil }
func (p *testDep2) Unload(_ *engine.Engine) error { return nil }
func (p *testDep2) Reload(_ *engine.Engine) error { return nil }

func (p *testDep3) Load(_ *engine.Engine) error   { return nil }
func (p *testDep3) Unload(_ *engine.Engine) error { return nil }
func (p *testDep3) Reload(_ *engine.Engine) error { return nil }

func TestExtractDependencies(t *testing.T) {
	p := &testPlugin{
		BasePlugin: NewBasePlugin("test"),
	}

	// 调试：打印结构体信息
	v := reflect.ValueOf(p).Elem()
	t.Logf("Plugin type: %s", v.Type().Name())
	t.Logf("Number of fields: %d", v.NumField())
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		tag := field.Tag.Get("inject")
		t.Logf("Field %d: %s, Type: %s, Tag: %q, Exported: %v", i, field.Name, field.Type, tag, field.IsExported())
	}

	deps := ExtractDependencies(p)
	t.Logf("Extracted deps: %v", deps)

	// 应该提取到 dep1 和 dep2，但不包括 dep3（没有标签）
	assert.Equal(t, 2, len(deps))
	assert.Contains(t, deps, "dep1")
	assert.Contains(t, deps, "dep2")
	assert.NotContains(t, deps, "dep3")
}

func TestInjectDependencies(t *testing.T) {
	p := &testPlugin{
		BasePlugin: NewBasePlugin("test"),
	}

	// 准备依赖
	dep1 := &testDep1{BasePlugin: NewBasePlugin("dep1")}
	dep2 := &testDep2{BasePlugin: NewBasePlugin("dep2")}

	deps := map[string]interface{}{
		"dep1": dep1,
		"dep2": dep2,
	}

	// 注入依赖
	err := InjectDependencies(p, deps)
	require.NoError(t, err)

	// 验证注入成功
	assert.NotNil(t, p.Dep1)
	assert.NotNil(t, p.Dep2)
	assert.Nil(t, p.Dep3) // 没有标签，不应该被注入
	assert.Equal(t, dep1, p.Dep1)
	assert.Equal(t, dep2, p.Dep2)
}

func TestInjectDependencies_MissingRequired(t *testing.T) {
	p := &testPlugin{
		BasePlugin: NewBasePlugin("test"),
	}

	// 只提供 dep1，缺少必需的 dep2
	deps := map[string]interface{}{
		"dep1": &testDep1{BasePlugin: NewBasePlugin("dep1")},
	}

	// 注入应该失败，因为 dep2 是 required
	err := InjectDependencies(p, deps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "required dependency")
	assert.Contains(t, err.Error(), "dep2")
}

func TestInjectDependencies_MissingOptional(t *testing.T) {
	p := &testPlugin{
		BasePlugin: NewBasePlugin("test"),
	}

	// 只提供 dep2（required），缺少可选的 dep1
	deps := map[string]interface{}{
		"dep2": &testDep2{BasePlugin: NewBasePlugin("dep2")},
	}

	// 注入应该成功，因为 dep1 是可选的
	err := InjectDependencies(p, deps)
	assert.NoError(t, err)

	// dep1 应该是 nil（可选依赖未提供）
	assert.Nil(t, p.Dep1)
	// dep2 应该被注入
	assert.NotNil(t, p.Dep2)
}

func TestGetDependencyFields(t *testing.T) {
	p := &testPlugin{
		BasePlugin: NewBasePlugin("test"),
	}

	fields := GetDependencyFields(p)

	// 应该找到 2 个依赖字段
	assert.Equal(t, 2, len(fields))

	// 检查字段信息
	var dep1Field, dep2Field *DependencyField
	for i := range fields {
		field := &fields[i]
		if field.PluginName == "dep1" {
			dep1Field = field
		} else if field.PluginName == "dep2" {
			dep2Field = field
		}
	}

	require.NotNil(t, dep1Field, "应该找到 dep1 字段")
	require.NotNil(t, dep2Field, "应该找到 dep2 字段")

	assert.Equal(t, "Dep1", dep1Field.Name)
	assert.False(t, dep1Field.Required, "dep1 应该是可选的")

	assert.Equal(t, "Dep2", dep2Field.Name)
	assert.True(t, dep2Field.Required, "dep2 应该是必需的")
}

func TestInjectDependencies_TypeMismatch(t *testing.T) {
	p := &testPlugin{
		BasePlugin: NewBasePlugin("test"),
	}

	// 提供错误类型的依赖
	deps := map[string]interface{}{
		"dep1": "wrong type", // 应该是 *testDep1，但提供了 string
		"dep2": &testDep2{BasePlugin: NewBasePlugin("dep2")},
	}

	// 注入应该失败，因为类型不匹配
	err := InjectDependencies(p, deps)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "type mismatch")
}

func TestExtractDependencies_NoTags(t *testing.T) {
	// 没有标签的插件
	type simplePlugin struct {
		*BasePlugin
		someField  string
		otherField int
	}

	p := &simplePlugin{
		BasePlugin: NewBasePlugin("simple"),
	}

	deps := ExtractDependencies(p)

	// 应该返回空切片
	assert.NotNil(t, deps)
	assert.Equal(t, 0, len(deps))
}

func TestExtractDependencies_OnlyMetadata(t *testing.T) {
	// 使用元数据声明依赖的插件
	metadata := &Metadata{
		Name:         "test",
		Dependencies: []string{"dep1", "dep2"},
	}

	p := NewBasePluginWithMetadata(metadata)

	// ExtractDependencies 只从标签提取，不读取元数据
	tagDeps := ExtractDependencies(p)
	assert.Equal(t, 0, len(tagDeps), "标签提取应该返回空（没有标签）")

	// Dependencies() 方法会从元数据读取
	metaDeps := p.Dependencies()
	assert.Equal(t, 2, len(metaDeps), "元数据应该包含 2 个依赖")
	assert.Contains(t, metaDeps, "dep1")
	assert.Contains(t, metaDeps, "dep2")
}
