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
	"fmt"
	"sync"
	"time"

	corectx "github.com/KomeiDiSanXian/remilia/core/context"
)

// State 表示有限状态机中的一个节点。是 string 的类型别名，用于类型安全的状态引用。
type State string

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
	// Timeout 指定会话的 TTL。超过此时间的会话会自动过期并清理。零值表示无超时。
	Timeout time.Duration
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

// Engine 管理 FSM 定义和会话迁移。并发安全：读用 RLock，写用 Lock。
// 会话通过 [Storage] 接口持久化（默认：[MemoryStorage]）。
type Engine struct {
	mu     sync.RWMutex
	fsms   map[string]*FSM
	stores Storage
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
	// 已有会话：检查是否能继续
	if existing := e.stores.Get(sessionID); existing != nil {
		e.mu.RLock()
		fsm, ok := e.fsms[existing.FSMName]
		e.mu.RUnlock()
		if ok && !fsm.canTransitionFrom(existing.Current) {
			e.stores.Delete(sessionID)
		} else {
			return false, nil
		}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, fsm := range e.fsms {
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
	existing := e.stores.Get(sessionID)
	if existing != nil {
		return fmt.Errorf("fsm: session %q already exists for FSM %q", sessionID, existing.FSMName)
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
func (e *Engine) EndSession(sessionID string) {
	e.stores.Delete(sessionID)
}

// GetSession 返回会话数据的副本，未找到或已过期时返回 nil。
func (e *Engine) GetSession(sessionID string) *Session {
	return e.stores.Get(sessionID)
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
