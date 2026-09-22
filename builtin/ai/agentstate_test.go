// Package ai agentstate_test.go — 代理状态归属表的守卫用例。
//
// 这些断言把"哪些状态属于代理、其中哪些跨重启保留"钉成可执行契约：
// 归属表与结构体字段一一对应，落库字段并集恰为持久化 schema，
// 会话内存态不进入序列化，且并发原语豁免名单不得夹带业务状态。
package ai

import (
	"reflect"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentStateSessionField 取 Session 上的字段描述，缺失即失败。
func agentStateSessionField(t *testing.T, name string) reflect.StructField {
	t.Helper()
	field, ok := reflect.TypeFor[session.Session]().FieldByName(name)
	require.Truef(t, ok, "Session has no field %q", name)
	return field
}

// agentStateCarriers 按承载结构聚合归属表里声明的字段名。
func agentStateCarriers() (sess, record, plugin map[string]string) {
	sess = map[string]string{}
	record = map[string]string{}
	plugin = map[string]string{}
	for _, item := range agentStateInventory() {
		for _, name := range item.Session {
			sess[name] = item.Name
		}
		for _, name := range item.Record {
			record[name] = item.Name
		}
		for _, name := range item.Plugin {
			plugin[name] = item.Name
		}
	}
	return sess, record, plugin
}

// TestAgentStateInventoryCarriersExist 归属表引用的承载字段必须真实存在，
// 且每一项都要有业务语义名、归属说明与至少一个承载字段。
func TestAgentStateInventoryCarriersExist(t *testing.T) {
	sessionType := reflect.TypeFor[session.Session]()
	recordType := reflect.TypeFor[session.Record]()
	pluginType := reflect.TypeFor[Plugin]()

	for _, item := range agentStateInventory() {
		require.NotEmptyf(t, item.Name, "state entry must carry a business name")
		require.NotEmptyf(t, item.Note, "state %q must explain its ownership", item.Name)
		assert.NotEmptyf(t, len(item.Session)+len(item.Record)+len(item.Plugin),
			"state %q must declare at least one carrier field", item.Name)

		for _, name := range item.Session {
			_, ok := sessionType.FieldByName(name)
			assert.Truef(t, ok, "state %q references missing Session field %q", item.Name, name)
		}
		for _, name := range item.Record {
			_, ok := recordType.FieldByName(name)
			assert.Truef(t, ok, "state %q references missing Record field %q", item.Name, name)
		}
		for _, name := range item.Plugin {
			_, ok := pluginType.FieldByName(name)
			assert.Truef(t, ok, "state %q references missing Plugin field %q", item.Name, name)
		}
	}
}

// TestAgentStateClassesMatchCarriers 分类必须与承载形态一致：落库状态要有
// Record 承载，会话内存态只挂 Session，进程级状态只挂 Plugin。
func TestAgentStateClassesMatchCarriers(t *testing.T) {
	for _, item := range agentStateInventory() {
		switch item.Class {
		case agentStatePersisted:
			assert.NotEmptyf(t, item.Record, "persisted state %q must name a record carrier", item.Name)
			assert.Emptyf(t, item.Plugin, "persisted state %q must not live on Plugin", item.Name)
		case agentStateSessionMemory:
			assert.NotEmptyf(t, item.Session, "session state %q must name Session carriers", item.Name)
			assert.Emptyf(t, item.Record, "session state %q must not be persisted", item.Name)
			assert.Emptyf(t, item.Plugin, "session state %q must not live on Plugin", item.Name)
		case agentStateProcessMemory:
			assert.NotEmptyf(t, item.Plugin, "process state %q must name Plugin carriers", item.Name)
			assert.Emptyf(t, item.Session, "process state %q must not live on Session", item.Name)
			assert.Emptyf(t, item.Record, "process state %q must not be persisted", item.Name)
		default:
			t.Errorf("state %q has unknown class %d", item.Name, item.Class)
		}
	}
}

// TestAgentStateInventoryUnique 一项业务状态只登记一次，一个承载字段只归属一项。
func TestAgentStateInventoryUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, item := range agentStateInventory() {
		assert.Falsef(t, seen[item.Name], "duplicate state entry %q", item.Name)
		seen[item.Name] = true
	}

	sess, record, plugin := agentStateCarriers()
	assertCarrierOwnersUnique(t, "Session", sess, agentStateInventory(), func(i agentStateItem) []string { return i.Session })
	assertCarrierOwnersUnique(t, "Record", record, agentStateInventory(), func(i agentStateItem) []string { return i.Record })
	assertCarrierOwnersUnique(t, "Plugin", plugin, agentStateInventory(), func(i agentStateItem) []string { return i.Plugin })
}

// assertCarrierOwnersUnique 校验同一承载结构上的字段不被多项状态重复声明。
func assertCarrierOwnersUnique(t *testing.T, carrier string, owners map[string]string, items []agentStateItem, fields func(agentStateItem) []string) {
	t.Helper()
	count := map[string]int{}
	for _, item := range items {
		for _, name := range fields(item) {
			count[name]++
		}
	}
	for name, n := range count {
		assert.Equalf(t, 1, n, "%s field %q is claimed by %d states (owner %q)", carrier, name, n, owners[name])
	}
}

// TestAgentStatePersistedCoversRecordSchema 落库状态的 Record 并集必须恰为持久化
// schema：归属表即持久化边界的唯一事实来源，新增列却忘记登记会被拦下。
func TestAgentStatePersistedCoversRecordSchema(t *testing.T) {
	want := map[string]bool{}
	typ := reflect.TypeFor[session.Record]()
	for field := range typ.Fields() {
		want[field.Name] = true
	}

	got := map[string]bool{}
	for _, item := range agentStateInventory() {
		if item.Class != agentStatePersisted {
			continue
		}
		for _, name := range item.Record {
			got[name] = true
		}
	}

	assert.Equalf(t, want, got, "persisted carriers must match the Session record schema exactly")
}

// agentStateInfrastructure Session 上只做并发保护、不承载业务状态的字段。
// 归属表覆盖的是业务状态；这些字段在此显式豁免，且必须保持为纯同步原语
// （守卫用例会校验类型），避免用豁免名单夹带真实状态。
//
// 豁免名单只服务于下面这条守卫用例，因此与用例同址，不进入生产代码。
func agentStateInfrastructure() []string {
	return []string{"mu", "turnMu"}
}

// TestAgentStateCoversSessionState Session 上每个字段要么被归属表覆盖，要么落
// 在并发原语豁免名单里；豁免名单只允许出现纯同步原语，避免夹带业务状态。
func TestAgentStateCoversSessionState(t *testing.T) {
	covered, _, _ := agentStateCarriers()

	exempt := map[string]bool{}
	for _, name := range agentStateInfrastructure() {
		exempt[name] = true
		field := agentStateSessionField(t, name)
		assert.Equalf(t, reflect.TypeFor[sync.Mutex](), field.Type,
			"infrastructure exemption %q may only cover a sync.Mutex, got %s", name, field.Type)
	}

	typ := reflect.TypeFor[session.Session]()
	for field := range typ.Fields() {
		name := field.Name
		if covered[name] != "" || exempt[name] {
			continue
		}
		t.Errorf("Session field %q is neither registered agent state nor a concurrency primitive", name)
	}
}

// TestAgentStateNonPersistedFieldsStayHiddenFromJSON 非落库的 Session 字段不得
// 进入 JSON：必须是未导出字段，或显式标注 json:"-"。
func TestAgentStateNonPersistedFieldsStayHiddenFromJSON(t *testing.T) {
	persisted := map[string]bool{}
	for _, item := range agentStateInventory() {
		if item.Class != agentStatePersisted {
			continue
		}
		for _, name := range item.Session {
			persisted[name] = true
		}
	}

	typ := reflect.TypeFor[session.Session]()
	for field := range typ.Fields() {
		if persisted[field.Name] || !field.IsExported() {
			continue
		}
		assert.Equalf(t, "-", field.Tag.Get("json"),
			"non-persisted Session field %q must be tagged json:\"-\"", field.Name)
	}
}

// TestAgentStatePersistenceRoundTrip 落库状态跨重启逐项还原，
