// pluginparts_test.go — 字段分区的守卫用例。
//
// 分区是"每个字段只有一个 owner"的可执行契约：新增字段若忘记登记、或把同一
// 字段名放进两个 owner（会让 p.x 提升二义），都会被这里拦下。
package ai

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pluginStateOwners 字段分区的唯一对照表：owner 结构名 → 它承载的字段名。
func pluginStateOwners() map[string][]string {
	return map[string][]string{
		"catalogState":   {"coord", "reg", "catalogGen", "skillReg", "perms", "cmdMu", "cmdPatterns"},
		"contextState":   {"history", "emb", "memory"},
		"executionState": {"syncer", "realCmdMu", "approvals"},
		"runtimeState":   {"sm", "triggerCmd", "defOnce", "def", "lifecycleCtx", "lifecycleCancel"},
		"adminState": {
			"fsmEngine", "reminders", "todos", "groupPolicies",
			"summaryMu", "summaries", "actionMu", "actionRate",
		},
	}
}

// pluginDirectFields 由 Plugin 自己持有、不归属任何 owner 的共享依赖。
func pluginDirectFields() []string {
	return []string{"cfg", "prov"}
}

// TestPluginStateOwnersAreEmbedded 每个声明过的 owner 都必须是 Plugin 的匿名
// 字段（嵌入），且其承载字段与对照表逐项一致——分区表即字段归属的事实来源。
func TestPluginStateOwnersAreEmbedded(t *testing.T) {
	typ := reflect.TypeFor[Plugin]()
	for owner, fields := range pluginStateOwners() {
		field, ok := typ.FieldByName(owner)
		require.Truef(t, ok, "owner %q must be a Plugin field", owner)
		assert.Truef(t, field.Anonymous, "owner %q must be embedded so p.<field> keeps resolving", owner)
		assert.Equalf(t, reflect.Struct, field.Type.Kind(), "owner %q must be a struct", owner)

		got := map[string]bool{}
		for field0 := range field.Type.Fields() {
			got[field0.Name] = true
		}
		want := map[string]bool{}
		for _, name := range fields {
			want[name] = true
		}
		assert.Equalf(t, want, got, "owner %q carries a different field set than declared", owner)
	}
}

// TestPluginFieldsAreCovered Plugin 的每个字段要么是直属共享依赖，要么恰好属于
// 一个 owner：新增业务字段必须显式选择归属，不能默默留在装配根上。
func TestPluginFieldsAreCovered(t *testing.T) {
	covered := map[string]string{}
	owners := map[string]bool{}
	for owner, fields := range pluginStateOwners() {
		owners[owner] = true
		for _, name := range fields {
			assert.Emptyf(t, covered[name], "field %q claimed by both %q and %q", name, covered[name], owner)
			covered[name] = owner
		}
	}

	direct := map[string]bool{}
	for _, name := range pluginDirectFields() {
		direct[name] = true
	}

	typ := reflect.TypeFor[Plugin]()
	for field := range typ.Fields() {
		if field.Anonymous {
			assert.Truef(t, owners[field.Name],
				"embedded struct %q is not registered in the field partition", field.Name)
			continue
		}
		if direct[field.Name] {
			continue
		}
		assert.NotEmptyf(t, covered[field.Name],
			"Plugin field %q is neither a direct dependency nor owned by a state struct", field.Name)
	}
}

// TestPluginStateFieldNamesDoNotCollide 同一字段名不得出现在两个 owner 里，
// 否则 p.<field> 提升二义（编译期只在使用处报错，容易被漏掉）。
func TestPluginStateFieldNamesDoNotCollide(t *testing.T) {
	seen := map[string]string{}
	for owner, fields := range pluginStateOwners() {
		for _, name := range fields {
			if prev, ok := seen[name]; ok {
				t.Errorf("field %q appears in both %q and %q", name, prev, owner)
			}
			seen[name] = owner
		}
	}
}

// pluginOwnerTypes owner 结构名的唯一对照表（类型级）。
func pluginOwnerTypes() map[string]reflect.Type {
	return map[string]reflect.Type{
		"catalogState":   reflect.TypeFor[catalogState](),
		"contextState":   reflect.TypeFor[contextState](),
		"executionState": reflect.TypeFor[executionState](),
		"runtimeState":   reflect.TypeFor[runtimeState](),
		"adminState":     reflect.TypeFor[adminState](),
	}
}

// TestPluginOwnerMethodNamesDoNotCollide owner 结构体上的方法经匿名嵌入提升为
// *Plugin 的方法。两个 owner 定义同名方法会让 p.<方法>() 二义——Go 只在**调用处**
// 报错，无人调用时静默存在，因此这里主动拦下。字段名由上面两条用例守卫，方法名
// 同样需要。
//
// 注意 NumMethod 只统计导出方法，而提升后能被包外调用、或用于满足接口的恰恰是
// 这些导出方法，正是需要唯一的集合。
func TestPluginOwnerMethodNamesDoNotCollide(t *testing.T) {
	seen := map[string]string{}
	for owner, typ := range pluginOwnerTypes() {
		for method := range typ.Methods() {
			name := method.Name
			if prev, ok := seen[name]; ok {
				t.Errorf("method %q appears on both %q and %q; p.%s() would be ambiguous",
					name, prev, owner, name)
			}
			seen[name] = owner
		}
	}
}
