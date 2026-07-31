// Package fsm 提供简单、并发安全的有限状态机（FSM）引擎，
// 专为 IM 机器人多步骤对话流程设计。
//
// 与在 handler 内部手写 `if state == x` 不同，
// 通过 [FSM] 和 [Event] 声明式定义状态、事件、迁移和回调。
// [Engine] 负责管理会话、迁移、超时和清理。
//
// 基本用法：
//
//	mgr := fsm.NewManager(nil)
//	signup := &fsm.FSM{
//	    Name: "signup", Initial: "idle",
//	    Events: []fsm.Event{
//	        {Name: "start", From: "idle", To: "ask_name",
//	            Match:  func(ctx *corectx.Context) bool { return ctx.GetMessageContent() == "/signup" },
//	            Action: func(ctx *fsm.FSMContext) error {
//	                ctx.Reply(platform.TextMessage("请输入姓名："))
//	                return nil
//	            }},
//	    },
//	}
//	mgr.Register(&fsm.FSMDescriptor{Name: "signup", FSM: signup})
package fsm

import (
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
)

// State 表示有限状态机中的一个节点。是 string 的类型别名，用于类型安全的状态引用。
type State string

// ErrSessionExists 表示尝试为同一 sessionID 创建会话时该会话已存在。
// 调用方可通过 errors.Is(err, ErrSessionExists) 判断。
var ErrSessionExists = errors.New("fsm: session already exists")

// Event 定义 FSM 中的一条迁移规则。
//
// 当当前会话状态与 [From] 匹配且 [Match] 返回 true 时，
// FSM 引擎执行 [Action]（若非 nil），然后迁移到 [To]。
//
// 使用 From="*" 可匹配任意当前状态（通配符迁移）。
type Event struct {
	// Name 是此迁移的可读标识（用于错误信息）。
	Name string
	// From 是源状态。使用 "*" 可匹配任意状态。
	From State
	// To 是目标状态。为空表示终态，Action 执行后自动结束会话。
	To State
	// Match 是判断此事件是否触发的谓词。接收原始事件上下文（而非 FSMContext）。
	Match func(ctx *corectx.Context) bool
	// Action 是迁移过程中执行的副作用。
	// 接收 [FSMContext]，其中嵌入了原始上下文并携带当前会话数据。
	Action func(ctx *FSMContext) error
}

// FSM 是声明式的有限状态机定义。
type FSM struct {
	// Name 在 [Engine] 中唯一标识此 FSM。
	Name string
	// Initial 是每个新会话的起始状态。
	Initial State
	// Events 是按顺序排列的迁移规则列表。按顺序第一个匹配的事件胜出。
	Events []Event
	// OnEnter 在进入一个状态后调用（成功迁移后）。如果回调返回错误，迁移会被回滚。
	OnEnter map[State]func(ctx *FSMContext) error
	// OnExit 在离开一个状态前调用。如果回调返回错误，迁移会被中止。
	OnExit map[State]func(ctx *FSMContext) error
	// Timeout 指定会话的 TTL。默认自会话**创建**时刻起计——
	// TryTransition 不会刷新过期时间（长对话不会因持续活跃而续期）。
	// 超过此时间的会话会自动过期并清理。零值表示无超时。
	Timeout time.Duration
	// RefreshOnActivity 为 true 时启用滑动 TTL：每次成功迁移都把
	// 过期时间重置为 now+Timeout，长对话只要保持活跃就不会过期。
	// 仅在 Timeout > 0 时有意义；默认 false（保持既有语义）。
	RefreshOnActivity bool
}

// Validate 检查 FSM 定义的结构正确性：
//   - Name、Initial 和至少一个 Event 是必需的
//   - 每个 Event 必须有 Name、From、To 以及非 nil 的 Match
//   - 至少有一个 Event 的 From 等于 Initial 状态
//   - OnEnter/OnExit 中引用的状态必须出现在某个 Event 中
func (f *FSM) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("fsm: Name is required")
	}
	if f.Initial == "" {
		return fmt.Errorf("fsm: Initial state is required")
	}
	if len(f.Events) == 0 {
		return fmt.Errorf("fsm: at least one Event is required")
	}

	allStates := make(map[State]bool)
	hasInitial := false
	for _, ev := range f.Events {
		if ev.Name == "" {
			return fmt.Errorf("fsm: event with empty Name")
		}
		if ev.From == "" {
			return fmt.Errorf("fsm: event %q has empty From state", ev.Name)
		}
		if ev.Match == nil {
			return fmt.Errorf("fsm: event %q has nil Match func", ev.Name)
		}
		allStates[ev.From] = true
		if ev.To != "" {
			allStates[ev.To] = true
		}
		if ev.From == f.Initial {
			hasInitial = true
		}
	}
	if !hasInitial {
		return fmt.Errorf("fsm: no event transitions from initial state %q", f.Initial)
	}

	for state := range f.OnEnter {
		if !allStates[state] && state != f.Initial {
			return fmt.Errorf("fsm: OnEnter state %q not referenced in any event", state)
		}
	}
	for state := range f.OnExit {
		if !allStates[state] {
			return fmt.Errorf("fsm: OnExit state %q not referenced in any event", state)
		}
	}
	return nil
}

// canTransitionFrom 检查是否有 Event 的 From 匹配 s（或通配符 "*"）。
// 用于判断 session 是否卡在终态。若返回 false，TryStartSession 会清理并重新开始。
func (f *FSM) canTransitionFrom(s State) bool {
	for _, ev := range f.Events {
		if ev.From == s {
			return true
		}
	}
	return false
}

// Engine 管理 FSM 定义和会话迁移。
//
// 并发安全性：
//   - FSM 定义表（Register/Unregister/GetFSM）由 e.mu 保护；
//   - 同一 sessionID 的启动/迁移由 per-session 互斥锁串行化
//     （保护 Session.Current 与 Session.Data 免受并发读写），
//     不同 sessionID 之间完全并发。
//
// 重入约束：Event.Action / OnEnter / OnExit 回调在会话锁内执行——
// 回调中调用 [FSMContext.EndSession] 是安全的，但**不要**对同一 sessionID
// 再调用 TryTransition/TryStartSession/StartSession（会自死锁）。
type Engine struct {
	mu     sync.RWMutex
	fsms   map[string]*FSM
	stores Storage

	// sessionLocks 按 sessionID 维护互斥锁（sync.Map[string]*sync.Mutex）。
	// 条目常驻不回收：bot 场景下 sessionID 基数 = 会话/聊天数，
	// 每条 ~100B 的常驻成本可忽略，换来跨会话操作永不互相阻塞/死锁。
	sessionLocks sync.Map
}

// NewEngine 创建一个使用指定 [Storage] 后端的 FSM 引擎。
// 如果 storage 为 nil，默认使用 [NewMemoryStorage]。
func NewEngine(storage Storage) *Engine {
	if storage == nil {
		storage = NewMemoryStorage()
	}
	return &Engine{
		fsms:   make(map[string]*FSM),
		stores: storage,
	}
}

// sessionLock 返回 sessionID 对应的互斥锁（懒创建，常驻）。
func (e *Engine) sessionLock(sessionID string) *sync.Mutex {
	if v, ok := e.sessionLocks.Load(sessionID); ok {
		return v.(*sync.Mutex)
	}
	v, _ := e.sessionLocks.LoadOrStore(sessionID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Register 将 FSM 定义添加到引擎。注册前会校验 FSM。返回错误的情况：
//   - FSM 为 nil 或校验失败
//   - 同名 FSM 已经注册
func (e *Engine) Register(f *FSM) error {
	if f == nil {
		return fmt.Errorf("fsm: cannot register nil FSM")
	}
	if err := f.Validate(); err != nil {
		return fmt.Errorf("fsm: register %q: %w", f.Name, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.fsms[f.Name]; exists {
		return fmt.Errorf("fsm: FSM %q already registered", f.Name)
	}
	e.fsms[f.Name] = f
	return nil
}

// Unregister 按名称移除 FSM 定义。此 FSM 的已有会话在下次 [TryTransition] 调用时会失败。
func (e *Engine) Unregister(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.fsms, name)
}

// GetFSM 按名称返回 FSM 定义，未找到时返回 nil。
func (e *Engine) GetFSM(name string) *FSM {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.fsms[name]
}

// ListFSMs 返回所有已注册 FSM 定义的名称列表。
func (e *Engine) ListFSMs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.fsms))
	for n := range e.fsms {
		names = append(names, n)
	}
	return names
}

// TryStartSession 检查所有已注册 FSM 的启动事件。
// 若消息匹配某个 FSM 的 Initial 状态事件，自动创建会话并执行该迁移。
// 返回 true 表示成功启动了一个 FSM 会话。
//
// 与 [Engine.StartSession] 不同，此方法不需要调用方指定 FSM 名称——
// 它自动遍历所有 FSM 并尝试匹配启动条件。
// 适用于 Router 自动检测 FSM 启动，无需外部命令注册。
func (e *Engine) TryStartSession(ctx *corectx.Context, sessionID string) (bool, error) {
	if ctx == nil || sessionID == "" {
		return false, nil
	}

	// 串行化同一会话的启动/迁移（见 Engine 并发说明）
	lk := e.sessionLock(sessionID)
	lk.Lock()
	defer lk.Unlock()

	// 已有会话：检查是否能继续
	if existing := e.stores.Get(sessionID); existing != nil {
		e.mu.RLock()
		fsm, ok := e.fsms[existing.FSMName]
		e.mu.RUnlock()
		switch {
		case !ok:
			// 关联 FSM 已被 Unregister：清理孤儿会话。
			// 此前直接 return false，导致该 sessionID 在无 Timeout 时
			// 永久无法开启任何新会话。
			e.stores.Delete(sessionID)
		case !fsm.canTransitionFrom(existing.Current):
			e.stores.Delete(sessionID)
		default:
			return false, nil
		}
	}

	// 在锁内快照 FSM 列表，用户回调（Match/Action/OnEnter/OnExit）在 e.mu
	// 之外执行：此前整个遍历持有 e.mu.RLock，回调内调用 Register/Unregister
	// 会直接死锁，有写者排队时回调内再进任何 RLock 方法也可能死锁。
	e.mu.RLock()
	fsmList := make([]*FSM, 0, len(e.fsms))
	for _, f := range e.fsms {
		fsmList = append(fsmList, f)
	}
	e.mu.RUnlock()

	for _, fsm := range fsmList {
		for _, event := range fsm.Events {
			if event.From != fsm.Initial && event.From != "*" {
				continue
			}
			if !event.Match(ctx) {
				continue
			}
			now := time.Now().Unix()
			session := &Session{
				ID: sessionID, FSMName: fsm.Name, Current: fsm.Initial,
				Data: make(map[string]any), CreatedAt: now, UpdatedAt: now,
			}
			if fsm.Timeout > 0 {
				session.ExpireAt = time.Now().Add(fsm.Timeout).Unix()
			}
			e.stores.Save(session)
			fsmCtx := &FSMContext{
				Context: ctx, SessionID: sessionID, Current: fsm.Initial,
				Data: session.Data, FSM: fsm, engine: e,
			}
			if fn := fsm.OnExit[fsm.Initial]; fn != nil {
				if err := fn(fsmCtx); err != nil {
					e.stores.Delete(sessionID)
					return false, fmt.Errorf("fsm: OnExit initial %q: %w", fsm.Initial, err)
				}
			}
			if event.Action != nil {
				if err := event.Action(fsmCtx); err != nil {
					e.stores.Delete(sessionID)
					return false, fmt.Errorf("fsm: start action %q: %w", event.Name, err)
				}
			}
			if fsmCtx.ended || event.To == "" {
				e.stores.Delete(sessionID)
				return true, nil
			}
			session.Current = event.To
			session.UpdatedAt = time.Now().Unix()
			fsmCtx.Current = event.To
			if fn := fsm.OnEnter[event.To]; fn != nil {
				if err := fn(fsmCtx); err != nil {
					e.stores.Delete(sessionID)
					return false, fmt.Errorf("fsm: OnEnter start %q rollback: %w", event.To, err)
				}
			}
			e.stores.Save(session)
			return true, nil
		}
	}
	return false, nil
}

// TryTransition 尝试推进一个会话在 FSM 中迁移。
//
// 通过 sessionID 查找会话，找到其关联的 FSM 定义，
// 按顺序遍历事件，执行第一个匹配的迁移。
//
// 返回值：
//   - newState：迁移尝试后的状态
//   - ok：true 表示发生了迁移
//   - err：如果 OnExit、Action 或 OnEnter 返回了错误则非 nil
//
// 若未找到 sessionID 对应的会话，返回 ("", false, nil)。
func (e *Engine) TryTransition(ctx *corectx.Context, sessionID string) (State, bool, error) {
	if ctx == nil || sessionID == "" {
		return "", false, nil
	}

	// 串行化同一会话的迁移：并发事件（如用户连发两条消息）对同一 Session 的
	// Current/Data 读写在无锁时是数据竞争（Data 为 map，可能直接 panic）。
	// 会话在锁内重新读取，保证 check-act 原子。
	lk := e.sessionLock(sessionID)
	lk.Lock()
	defer lk.Unlock()

	session := e.stores.Get(sessionID)
	if session == nil {
		return "", false, nil
	}

	e.mu.RLock()
	fsm, exists := e.fsms[session.FSMName]
	e.mu.RUnlock()
	if !exists {
		return "", false, fmt.Errorf("fsm: FSM %q not found for session %q", session.FSMName, sessionID)
	}

	for _, event := range fsm.Events {
		if event.From != "*" && event.From != session.Current {
			continue
		}
		if !event.Match(ctx) {
			continue
		}
		fsmCtx := &FSMContext{
			Context:   ctx,
			SessionID: sessionID,
			Current:   session.Current,
			Data:      session.Data,
			FSM:       fsm,
			engine:    e,
		}
		if fn := fsm.OnExit[session.Current]; fn != nil {
			if err := fn(fsmCtx); err != nil {
				return session.Current, false, fmt.Errorf("fsm: OnExit %q: %w", session.Current, err)
			}
		}
		if event.Action != nil {
			if err := event.Action(fsmCtx); err != nil {
				return session.Current, false, fmt.Errorf("fsm: action %q: %w", event.Name, err)
			}
		}

		if fsmCtx.ended || event.To == "" {
			// ended=true（用户调了 EndSession）或 To 为空 → 终态，结束会话
			e.stores.Delete(sessionID)
			return session.Current, true, nil
		}

		prev := session.Current
		session.Current = event.To
		session.UpdatedAt = time.Now().Unix()
		// 滑动 TTL：活跃迁移刷新过期时间（见 FSM.RefreshOnActivity）
		if fsm.Timeout > 0 && fsm.RefreshOnActivity {
			session.ExpireAt = time.Now().Add(fsm.Timeout).Unix()
		}
		fsmCtx.Current = event.To
		if fn := fsm.OnEnter[event.To]; fn != nil {
			if err := fn(fsmCtx); err != nil {
				session.Current = prev
				return prev, false, fmt.Errorf("fsm: OnEnter %q rollback: %w", event.To, err)
			}
		}
		e.stores.Save(session)
		return event.To, true, nil
	}
	return session.Current, false, nil
}

// StartSession 创建一个新的 FSM 会话并设置其初始状态。
//
// sessionID 应唯一标识一个会话（例如 "platform:chatID"）。
// 如果 FSM 为初始状态定义了 OnEnter 处理器，会立即调用。
//
// 返回错误的情况：
//   - sessionID 为空
//   - fsmName 未注册
//   - 相同 sessionID 的会话已存在
//   - OnEnter 初始回调返回错误（会话创建会被回滚）
func (e *Engine) StartSession(ctx *corectx.Context, fsmName, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("fsm: sessionID is required")
	}
	e.mu.RLock()
	fsm, exists := e.fsms[fsmName]
	e.mu.RUnlock()
	if !exists {
		return fmt.Errorf("fsm: FSM %q not found", fsmName)
	}

	// 串行化同一会话的创建：两个并发 StartSession/TryStartSession
	// 不再可能互相覆盖对方刚创建的会话
	lk := e.sessionLock(sessionID)
	lk.Lock()
	defer lk.Unlock()

	existing := e.stores.Get(sessionID)
	if existing != nil {
		return fmt.Errorf("%w: session %q for FSM %q", ErrSessionExists, sessionID, existing.FSMName)
	}
	now := time.Now().Unix()
	session := &Session{
		ID:        sessionID,
		FSMName:   fsmName,
		Current:   fsm.Initial,
		Data:      make(map[string]any),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if fsm.Timeout > 0 {
		session.ExpireAt = time.Now().Add(fsm.Timeout).Unix()
	}
	e.stores.Save(session)
	if fn := fsm.OnEnter[fsm.Initial]; fn != nil {
		fsmCtx := &FSMContext{
			Context:   ctx,
			SessionID: sessionID,
			Current:   fsm.Initial,
			Data:      session.Data,
			FSM:       fsm,
			engine:    e,
		}
		if err := fn(fsmCtx); err != nil {
			e.stores.Delete(sessionID)
			return fmt.Errorf("fsm: OnEnter initial %q: %w", fsm.Initial, err)
		}
	}
	return nil
}

// EndSession 终止一个 FSM 会话并将其从存储中删除。
// 会话被永久删除；后续针对此 sessionID 的 [TryTransition] 调用将返回 ("", false, nil)。
//
// 本方法不获取会话锁（Storage 自身并发安全），因此可以在
// Action/OnEnter/OnExit 回调内安全调用（[FSMContext.EndSession] 即此路径）。
func (e *Engine) EndSession(sessionID string) {
	e.stores.Delete(sessionID)
}

// UpdateSessionData 在会话锁内对指定会话的 Data 应用变更函数并持久化。
//
// 这是从会话外部（如 StartSession 之后立即注入初始数据）修改 Data 的
// 唯一受支持方式——GetSession 返回的是副本，修改副本不会生效。
// 返回 false 表示会话不存在或已过期。
//
// fn 在该会话的互斥锁内执行：不要在 fn 内调用本 Engine 的其他会话方法
// （TryTransition/TryStartSession/StartSession/GetSession 等，会自死锁）。
func (e *Engine) UpdateSessionData(sessionID string, fn func(data map[string]any)) bool {
	if sessionID == "" || fn == nil {
		return false
	}
	lk := e.sessionLock(sessionID)
	lk.Lock()
	defer lk.Unlock()

	session := e.stores.Get(sessionID)
	if session == nil {
		return false
	}
	if session.Data == nil {
		session.Data = make(map[string]any)
	}
	fn(session.Data)
	session.UpdatedAt = time.Now().Unix()
	e.stores.Save(session)
	return true
}

// GetSession 返回会话数据的副本（含 Data 的浅拷贝），未找到或已过期时返回 nil。
//
// 返回值仅供检视：修改副本不影响存储中的会话
// （此前返回存储内的活指针，与文档"返回副本"不符，外部修改会绕过会话锁）。
//
// 注意：不要在 FSM 回调内对同一 sessionID 调用本方法（回调持有会话锁，会自死锁）；
// 回调内请直接使用 FSMContext.Current / FSMContext.Data。
func (e *Engine) GetSession(sessionID string) *Session {
	lk := e.sessionLock(sessionID)
	lk.Lock()
	defer lk.Unlock()

	s := e.stores.Get(sessionID)
	if s == nil {
		return nil
	}
	cp := *s
	cp.Data = make(map[string]any, len(s.Data))
	maps.Copy(cp.Data, s.Data)
	return &cp
}

// StartCleanup 启动后台 goroutine，定期从存储中清理过期会话。
// 当 stop 通道被关闭时 goroutine 停止。
//
// 如果 interval <= 0，默认使用 1 分钟。
func (e *Engine) StartCleanup(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.stores.Cleanup(time.Now().Unix())
			case <-stop:
				return
			}
		}
	}()
}

// nowUnix 返回当前 Unix 时间戳。
func nowUnix() int64 {
	return time.Now().Unix()
}
