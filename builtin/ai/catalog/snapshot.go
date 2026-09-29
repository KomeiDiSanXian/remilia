package catalog

import (
	"slices"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// snapshot.go — 目录快照：某一刻"当前系统有哪些动作"的不可变、可版本化视图。
//
// 目录是各来源（内置动作、插件工具、命令发现、外部工具服务器）聚合的结果。
// 来源可以动态变化，但选择与稳定策略只应看到一份固定的快照，从而不必感知任何
// 来源的运行时细节（连接、重连、协议）。快照与来源之间由目录装配层单点衔接：
//
//	来源（可变）→ 目录装配层（聚合）→ CatalogSnapshot（不可变）→ Selector（只读）
//
// Generation 只在目录成员关系变化（动作被登记或移除）时推进；来源的软不可用
// （暂时断开、重连中）不改变成员关系，因而不推进代数——这样连接抖动不会让
// 会话级选择缓存与前缀缓存失效。

// CatalogSnapshot 一份不可变的动作目录视图。
//
// 零值可用（空目录，Gener 为 0）。它是值语义：构造时深拷贝输入，调用方拿到的
// Actions 不会被后续注册影响。
type CatalogSnapshot struct {
	generation uint64
	actions    []toolkit.Action
}

// NewSnapshot 构造一份目录快照。actions 会被拷贝，调用方后续修改不影响快照。
func NewSnapshot(generation uint64, actions []toolkit.Action) CatalogSnapshot {
	return CatalogSnapshot{
		generation: generation,
		actions:    append([]toolkit.Action(nil), actions...),
	}
}

// Generation 返回目录代数（成员关系版本号）。
func (s CatalogSnapshot) Generation() uint64 { return s.generation }

// Actions 返回快照中的动作副本。
func (s CatalogSnapshot) Actions() []toolkit.Action {
	return append([]toolkit.Action(nil), s.actions...)
}

// Len 返回快照中的动作数量。
func (s CatalogSnapshot) Len() int { return len(s.actions) }

// Lookup 按模型函数名查找动作。
func (s CatalogSnapshot) Lookup(name string) (toolkit.Action, bool) {
	for _, a := range s.actions {
		if a.Spec.Name == name {
			return a, true
		}
	}
	return toolkit.Action{}, false
}

// SortedNames 返回按名称升序排列的动作名，便于观测与稳定断言。
//
// 快照本身保持输入顺序（由注册表的名称排序保证稳定）；本方法只用于需要显式
// 名称列表的场合。
func (s CatalogSnapshot) SortedNames() []string {
	names := make([]string, 0, len(s.actions))
	for _, a := range s.actions {
		names = append(names, a.Spec.Name)
	}
	slices.SortFunc(names, strings.Compare)
	return names
}
