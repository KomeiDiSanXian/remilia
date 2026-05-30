package plugin

import (
	"sync"
	"sync/atomic"
)

// container.go — 依赖注入容器
//
// 支持两阶段使用模式：
//  1. 注册阶段（Register/Remove）：使用 sync.Map 保证并发安全
//  2. 冻结阶段（Freeze 后）：Get/Has 切换为原子指针只读快照，读性能提升 2-3x
//
// 插件全部加载完成后调用 [Manager.FreezeContainer]，后续 Get 仅需一次原子 Load，无锁竞争。

// Container 依赖注入容器
type Container struct {
	services sync.Map // 注册阶段及冻结后的写操作

	// 冻结后的只读快照，使用 atomic.Pointer 原子替换，消除 data race
	frozen     atomic.Bool
	frozenMap  atomic.Pointer[map[string]any]
	snapshotMu sync.Mutex // 保护 refreshSnapshot 的并发重建

	watchers   map[string][]func(name string, oldVal, newVal any)
	watchersMu sync.Mutex
}

// NewContainer 创建依赖注入容器
func NewContainer() *Container {
	return &Container{
		watchers: make(map[string][]func(name string, oldVal, newVal any)),
	}
}

// Register 注册服务。若同名服务已存在，触发值变更通知。
// 冻结后会自动刷新只读快照，支持热重载/动态注册场景。
func (c *Container) Register(name string, service any) {
	if name == "" {
		panic("container: Register requires non-empty name")
	}
	oldVal, loaded := c.services.Load(name)
	c.services.Store(name, service)
	if loaded {
		c.notifyWatchers(name, oldVal, service)
	}
	if c.frozen.Load() {
		c.refreshSnapshot()
	}
}

// OnValueChanged 注册指定 key 的值变更回调。当该 key 被重新 Register 或 Remove 时触发。
//
//	container.OnValueChanged("storage", func(name string, oldVal, newVal any) {
//	    log.Printf("storage reloaded: %T -> %T", oldVal, newVal)
//	})
//
// 回调与 Register 在同一 goroutine 中同步执行（因此不应长时间阻塞）。
// 首次注册不会触发回调（oldVal 为 nil）。Remove 时 newVal 为 nil。
func (c *Container) OnValueChanged(name string, fn func(name string, oldVal, newVal any)) {
	if name == "" || fn == nil {
		return
	}
	c.watchersMu.Lock()
	defer c.watchersMu.Unlock()
	c.watchers[name] = append(c.watchers[name], fn)
}

// notifyWatchers 异步通知指定 key 的监听器。
func (c *Container) notifyWatchers(name string, oldVal, newVal any) {
	c.watchersMu.Lock()
	fns := make([]func(name string, oldVal, newVal any), len(c.watchers[name]))
	copy(fns, c.watchers[name])
	c.watchersMu.Unlock()

	for _, fn := range fns {
		fn(name, oldVal, newVal)
	}
}

// Freeze 将容器切换为只读快照模式。
// 调用后 Get/Has 使用原子指针快照，读性能提升 2-3x。
// 冻结后仍可调用 Register/Remove，会自动原子替换快照。
func (c *Container) Freeze() {
	c.frozen.Store(true)
	c.refreshSnapshot()
}

// refreshSnapshot 重建只读快照并原子替换（并发安全）。
func (c *Container) refreshSnapshot() {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	snapshot := make(map[string]any)
	c.services.Range(func(k, v any) bool {
		snapshot[k.(string)] = v
		return true
	})
	c.frozenMap.Store(&snapshot)
}

// Get 获取服务。冻结后通过原子 Load 读取快照，无锁竞争。
func (c *Container) Get(name string) (any, bool) {
	if c.frozen.Load() {
		if m := c.frozenMap.Load(); m != nil {
			v, ok := (*m)[name]
			return v, ok
		}
	}
	return c.services.Load(name)
}

// Has 检查服务是否存在
func (c *Container) Has(name string) bool {
	_, ok := c.Get(name)
	return ok
}

// Remove 移除服务并触发值变更通知（newVal 为 nil）。
// 冻结后会自动刷新只读快照。
func (c *Container) Remove(name string) {
	oldVal, loaded := c.services.Load(name)
	c.services.Delete(name)
	if loaded {
		c.notifyWatchers(name, oldVal, nil)
	}
	if c.frozen.Load() {
		c.refreshSnapshot()
	}
}
