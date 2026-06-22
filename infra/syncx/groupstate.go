package syncx

// GroupStateStore 是泛型的 per-group 状态管理器，基于 [Map][string, *T] 构建。
//
// 相比直接使用 Map[string, *T]，额外提供无参的 [GroupStateStore.GetOrCreate] 便捷方法
// （自动以 new(T) 作为缺失 key 的构造函数，无需调用方传入工厂函数）。
//
// 其余方法（Load / Delete / DeleteIf / Range / Len 等）由嵌入的 Map 直接提供，
// 不再重复实现。
//
// 典型用法（词语接龙/钓鱼/游戏等 per-group 状态）：
//
//	type gameState struct {
//	    mu     sync.Mutex
//	    active bool
//	    round  int
//	}
//
//	var store syncx.GroupStateStore[gameState]
//
//	state := store.GetOrCreate(groupID)
//	state.mu.Lock()
//	defer state.mu.Unlock()
//	state.active = true
//
// 注意：T 本身的并发保护由调用方负责（通常在 T 内内嵌 sync.Mutex）。
// 零值直接可用，使用后不得复制。
type GroupStateStore[T any] struct {
	Map[string, *T]
}

// GetOrCreate 返回 groupID 对应的状态指针。
//
// 若该 groupID 不存在，自动创建零值 *T 并存储后返回。
// 多个 goroutine 并发调用是安全的；同一 key 只会创建一次。
func (s *GroupStateStore[T]) GetOrCreate(groupID string) *T {
	return s.Map.GetOrCreate(groupID, func() *T { return new(T) })
}

// Get 是 [Map.Load] 的别名
func (s *GroupStateStore[T]) Get(groupID string) (*T, bool) {
	return s.Load(groupID)
}
