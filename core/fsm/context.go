package fsm

import corectx "github.com/KomeiDiSanXian/remilia/core/context"

// FSMContext 是传递给 [Event.Action]、[FSM.OnEnter] 和 [FSM.OnExit] 回调的上下文。
//
// 它嵌入 [corectx.Context]，因此 [corectx.Context.Reply]、
// [corectx.Context.GetMessageContent] 等方法可直接使用。
//
// # 终态规则
//
// 会话在 Action 或 OnEnter 结束后，按以下规则确定是否结束：
//
//   - event.To == "" 且 ended == true  → 终态（双保险，无冲突）
//   - event.To == "" 且 ended == false → 终态（框架自动 Delete）
//   - event.To != "" 且 ended == true  → 终态（用户显式 EndSession 优先于 To）
//   - event.To != "" 且 ended == false → 正常迁移到 To
//
// 推荐做法：终态事件省略 To，框架自动结束会话，无需调 EndSession。
// 若在 Action 中显式调用 EndSession，无论 To 是否为空都会结束会话。
//
// # 并发与重入
//
// 回调在该会话的互斥锁内执行：
//   - [FSMContext.EndSession] 可以安全调用；
//   - 不要在回调内对**同一** sessionID 调用 Engine 的
//     TryTransition/TryStartSession/StartSession/GetSession（会自死锁）；
//     对其他 sessionID 的调用是安全的（每个会话一把独立锁）。
//   - Data 仅在回调执行期间受锁保护，请勿把它保存到回调之外异步使用。
type FSMContext struct {
	*corectx.Context
	// SessionID 是此 FSM 会话的唯一标识。
	SessionID string
	// Current 是回调被调用时的当前状态。
	Current State
	// Data 是会话的用户定义键值存储。在此做的修改会在迁移之间持久化。
	Data map[string]any
	// FSM 是此会话所属的 FSM 定义。
	FSM *FSM

	engine *Engine
	ended  bool
}

// EndSession 结束当前 FSM 会话。调用后无论 event.To 是否为空，框架都会结束会话。
func (ctx *FSMContext) EndSession() {
	ctx.ended = true
	if ctx.engine != nil {
		ctx.engine.EndSession(ctx.SessionID)
	}
}
