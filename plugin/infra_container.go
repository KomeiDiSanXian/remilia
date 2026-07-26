package plugin

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

// container.go — 依赖注入容器（类型索引版）
//
// 除 string 键值访问外，容器维护 reflect.Type → []name 反向索引，
// 支持通过 [GetService[T]] 按类型自动解析。

// serviceEntry 容器中的一个注册条目。
type serviceEntry struct {
	name  string
	value any
	typ   reflect.Type
}

// Container 依赖注入容器
type Container struct {
	services sync.Map // name → *serviceEntry

	// regMu 串行化 Register/Remove 的 check-then-act 序列，
	// 避免同名并发注册时漏掉 typeIndex 清理或 watcher 通知。
	regMu sync.Mutex

	// typeIndex 类型索引：reflect.Type → []*serviceEntry
	typeIndex   sync.Map
	typeIndexMu sync.Mutex

	// 冻结后的只读快照
	frozen     atomic.Bool
	frozenMap  atomic.Pointer[map[string]any]
	snapshotMu sync.Mutex

	watchers   map[string][]func(name string, oldVal, newVal any)
	watchersMu sync.Mutex
}

// NewContainer 创建依赖注入容器
func NewContainer() *Container {
	return &Container{
		watchers: make(map[string][]func(name string, oldVal, newVal any)),
	}
}

// Register 注册服务。自动存储类型信息并更新类型索引。
// 若同名服务已存在，触发值变更通知。
// 冻结后会自动刷新只读快照。
func (c *Container) Register(name string, service any) {
	if name == "" {
		panic("container: Register requires non-empty name")
	}

	entry := &serviceEntry{
		name:  name,
		value: service,
		typ:   reflect.TypeOf(service),
	}

	// regMu 保证 Load+Store+索引维护的原子性；watcher 通知在锁外执行
	c.regMu.Lock()
	oldRaw, loaded := c.services.Load(name)
	c.services.Store(name, entry)

	// 更新类型索引
	if entry.typ != nil {
		c.addToTypeIndex(entry)
	}

	var oldVal any
	if loaded {
		oldEntry := oldRaw.(*serviceEntry)
		if oldEntry.typ != entry.typ {
			c.removeFromTypeIndex(oldEntry)
		}
		oldVal = oldEntry.value
	}
	c.regMu.Unlock()

	if loaded {
		c.notifyWatchers(name, oldVal, service)
	}

	if c.frozen.Load() {
		c.refreshSnapshot()
	}
}

// typeIndexKey 获取 T 的类型索引键。
func typeIndexKey[T any]() reflect.Type {
	return reflect.TypeFor[T]()
}

// addToTypeIndex 将条目加入类型索引。
func (c *Container) addToTypeIndex(entry *serviceEntry) {
	c.typeIndexMu.Lock()
	defer c.typeIndexMu.Unlock()
	raw, _ := c.typeIndex.Load(entry.typ)
	entries, _ := raw.([]*serviceEntry)
	for i, e := range entries {
		if e.name == entry.name {
			entries[i] = entry
			c.typeIndex.Store(entry.typ, entries)
			return
		}
	}
	c.typeIndex.Store(entry.typ, append(entries, entry))
}

// removeFromTypeIndex 从类型索引移除条目。
func (c *Container) removeFromTypeIndex(entry *serviceEntry) {
	c.typeIndexMu.Lock()
	defer c.typeIndexMu.Unlock()
	raw, ok := c.typeIndex.Load(entry.typ)
	if !ok {
		return
	}
	entries := raw.([]*serviceEntry)
	filtered := make([]*serviceEntry, 0, len(entries))
	for _, e := range entries {
		if e.name != entry.name {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		c.typeIndex.Delete(entry.typ)
	} else {
		c.typeIndex.Store(entry.typ, filtered)
	}
}

// Get 通过 name 获取服务值（保留原语义，返回 any）。
func (c *Container) Get(name string) (any, bool) {
	if c.frozen.Load() {
		if m := c.frozenMap.Load(); m != nil {
			v, ok := (*m)[name]
			return v, ok
		}
	}
	raw, ok := c.services.Load(name)
	if !ok {
		return nil, false
	}
	return raw.(*serviceEntry).value, true
}

// Has 检查服务是否存在
func (c *Container) Has(name string) bool {
	_, ok := c.Get(name)
	return ok
}

// GetService 通过 name 获取类型安全的服务值。
// 若 name 为空，按类型自动解析（要求唯一匹配）。
func GetService[T any](c *Container, name string) (T, bool) {
	var zero T
	if name != "" {
		v, ok := c.Get(name)
		if !ok {
			return zero, false
		}
		tv, ok := v.(T)
		return tv, ok
	}
	// 按类型解析
	entries := lookupServiceType[T](c)
	if len(entries) == 0 {
		return zero, false
	}
	if len(entries) > 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.name
		}
		panic(fmt.Sprintf("container: multiple services of type %T found: %v; use GetService[T](c, name)", zero, names))
	}
	tv, ok := entries[0].value.(T)
	return tv, ok
}

// MustGetService 同 GetService，不存在或类型不匹配时 panic。
func MustGetService[T any](c *Container, name string) T {
	v, ok := GetService[T](c, name)
	if !ok {
		panic(fmt.Sprintf("container: service %T(%q) not found", *new(T), name))
	}
	return v
}

// ListServices 返回所有实现了 T 的服务（name → value）。
func ListServices[T any](c *Container) map[string]T {
	entries := lookupServiceType[T](c)
	result := make(map[string]T, len(entries))
	for _, e := range entries {
		if tv, ok := e.value.(T); ok {
			result[e.name] = tv
		}
	}
	return result
}

// lookupServiceType 从类型索引查找所有匹配的条目（泛型版）。
func lookupServiceType[T any](c *Container) []*serviceEntry {
	return lookupServiceTypeByReflect(c, typeIndexKey[T]())
}

// lookupServiceTypeByReflect 从类型索引查找（非泛型版，供三色 DryRun 的 pending 类型使用）。
func lookupServiceTypeByReflect(c *Container, typ reflect.Type) []*serviceEntry {
	c.typeIndexMu.Lock()
	defer c.typeIndexMu.Unlock()
	raw, ok := c.typeIndex.Load(typ)
	if !ok {
		return nil
	}
	entries := raw.([]*serviceEntry)
	active := make([]*serviceEntry, 0, len(entries))
	for _, e := range entries {
		if _, loaded := c.services.Load(e.name); loaded {
			active = append(active, e)
		}
	}
	return active
}

// OnValueChanged 注册指定 key 的值变更回调。
func (c *Container) OnValueChanged(name string, fn func(name string, oldVal, newVal any)) {
	if name == "" || fn == nil {
		return
	}
	c.watchersMu.Lock()
	defer c.watchersMu.Unlock()
	c.watchers[name] = append(c.watchers[name], fn)
}

func (c *Container) notifyWatchers(name string, oldVal, newVal any) {
	c.watchersMu.Lock()
	fns := make([]func(name string, oldVal, newVal any), len(c.watchers[name]))
	copy(fns, c.watchers[name])
	c.watchersMu.Unlock()
	for _, fn := range fns {
		fn(name, oldVal, newVal)
	}
}

// Freeze 锁住只读快照。
func (c *Container) Freeze() {
	c.frozen.Store(true)
	c.refreshSnapshot()
}

func (c *Container) refreshSnapshot() {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	snapshot := make(map[string]any)
	c.services.Range(func(k, v any) bool {
		snapshot[k.(string)] = v.(*serviceEntry).value
		return true
	})
	c.frozenMap.Store(&snapshot)
}

// Remove 移除服务。
func (c *Container) Remove(name string) {
	c.regMu.Lock()
	raw, loaded := c.services.Load(name)
	if !loaded {
		c.regMu.Unlock()
		return
	}
	entry := raw.(*serviceEntry)
	c.services.Delete(name)
	c.removeFromTypeIndex(entry)
	c.regMu.Unlock()

	c.notifyWatchers(name, entry.value, nil)
	if c.frozen.Load() {
		c.refreshSnapshot()
	}
}
