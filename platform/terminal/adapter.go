package terminal

import (
	"bufio"
	stdctx "context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"golang.org/x/term"
)

const (
	PlatformID = "terminal"

	DefaultUserID  = "terminal-user"
	DefaultBotID   = "terminal-bot"
	DefaultBotName = "TerminalBot"

	DefaultPrompt         = "User> "
	DefaultWelcomeMessage = "=== 终端 Bot 控制台 ===\n输入 'quit' 或 'exit' 退出。\n"
)

type SentMessage struct {
	ID        string
	Content   string
	Target    platform.ChatInfo
	Edited    bool
	Deleted   bool
	Reactions []platform.Emoji
}

// Adapter 是终端平台的适配器实现。
type Adapter struct {
	reader         io.Reader
	writer         io.Writer
	completionFunc func(string) []string
	rawOldState    *term.State
	// termOut 是 raw 模式下的行编辑器；非 nil 时所有输出都必须经由它。
	//
	// term.MakeRaw 会清掉 OPOST/ONLCR，裸 "\n" 只发 LF 而不回到行首，
	// 直接写 a.writer 会产生阶梯状换行；而且 term.Terminal 内部维护着
	// 光标位置模型并用自己的锁串行化写入，绕过它还会在机器人回复与用户
	// 正在输入的行交错时把屏幕状态彻底搞乱。
	termOut *term.Terminal
	handler func(platform.Event)
	// stopCh 与 stopped 均由 a.mu 保护，且每次 Start 都会重置，
	// 以支持 Stop → Start 的重启循环。
	stopCh       chan struct{}
	prompt       string
	welcomeMsg   string
	botID        string
	botName      string
	lastCompLine string
	platform.DisconnectNotifier
	messages           []*SentMessage
	lastCompCandidates []string
	msgCount           atomic.Uint64
	mu                 sync.Mutex
	msgMu              sync.Mutex
	running            atomic.Bool
	stopped            bool
}

type CompletionFunc func(prefix string) []string

type Option func(*Adapter)

func WithInput(r io.Reader) Option {
	return func(a *Adapter) { a.reader = r }
}
func WithOutput(w io.Writer) Option {
	return func(a *Adapter) { a.writer = w }
}
func WithPrompt(p string) Option {
	return func(a *Adapter) { a.prompt = p }
}
func WithWelcomeMessage(msg string) Option {
	return func(a *Adapter) { a.welcomeMsg = msg }
}
func WithBotID(id string) Option {
	return func(a *Adapter) { a.botID = id }
}
func WithBotName(name string) Option {
	return func(a *Adapter) { a.botName = name }
}
func WithCompletionFunc(fn CompletionFunc) Option {
	return func(a *Adapter) { a.completionFunc = fn }
}

func NewAdapter(opts ...Option) *Adapter {
	a := &Adapter{
		reader:     os.Stdin,
		writer:     os.Stdout,
		prompt:     DefaultPrompt,
		welcomeMsg: DefaultWelcomeMessage,
		botID:      DefaultBotID,
		botName:    DefaultBotName,
		stopCh:     make(chan struct{}),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *Adapter) Platform() string {
	return PlatformID
}

// Start 启动终端输入循环。
//
// 当 stdin 是真实终端且可设为原始模式时，启用方向键历史（term.Terminal）和
// Tab 补全；否则回退到 bufio.Scanner 保证跨平台兼容。
func (a *Adapter) Start(ctx stdctx.Context, handler func(platform.Event)) error {
	if !a.running.CompareAndSwap(false, true) {
		return fmt.Errorf("terminal adapter: 已经处于运行状态")
	}
	defer a.running.Store(false)

	a.mu.Lock()
	a.handler = handler
	// 仅在确实经历过 Stop 之后才重建 stopCh：它原本只在 NewAdapter 里创建一次，
	// Stop() 关闭后就永久保持关闭状态。于是 Bot.Restart()（Stop 后用同一批
	// adapter 实例再 Start）会让下面的输入循环在第一次迭代就立刻退出——
	// 适配器在任何一次重启后即静默失效，而 Start 的返回值在 goroutine 里被
	// 丢弃，外部完全看不出来。
	//
	// 加 if 判断而非无条件重建：无条件重建会把"Start 之后才到达的 Stop"
	// 与"Start 之前就到达的 Stop"混为一谈，让前者被静默吞掉。
	if a.stopped {
		a.stopCh = make(chan struct{})
		a.stopped = false
	}
	// 捕获本轮运行的 stopCh，避免下游读到后续 Start 重建出的新 channel。
	stopCh := a.stopCh
	a.mu.Unlock()

	_, _ = fmt.Fprint(a.writer, a.welcomeMsg)

	// 检查 stdin 是否为真实终端，尝试设置原始模式
	if a.tryRawMode() {
		return a.startTerm(ctx, stopCh)
	}
	return a.startScanner(ctx, stopCh)
}

// tryRawMode 尝试将终端设为原始模式，成功返回 true。
func (a *Adapter) tryRawMode() bool {
	if a.reader != os.Stdin {
		return false
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		_, _ = fmt.Fprintf(a.writer, "[Terminal] 提示: 终端原始模式不可用 (%v)，方向键和 Tab 补全将不支持\n", err)
		return false
	}
	a.rawOldState = oldState

	// Windows 上启用 VT100 转义序列支持（refreshLine 的擦除/重绘需要）。
	// 必须用 Stdout：VT 处理是**输出**句柄的标志位，详见 enableVT100 注释。
	enableVT100(os.Stdout.Fd())

	return true
}

// output 写入一行终端输出。
//
// raw 模式下经由 term.Terminal（负责 CRLF 转换、加锁与提示符重绘），
// 否则退回裸 writer。内容一律先经 sanitizeForTerminal 过滤控制序列。
func (a *Adapter) output(format string, args ...any) {
	line := sanitizeForTerminal(fmt.Sprintf(format, args...))

	a.mu.Lock()
	t := a.termOut
	a.mu.Unlock()

	if t != nil {
		_, _ = t.Write([]byte(line))
		return
	}
	_, _ = fmt.Fprint(a.writer, line)
}

// restoreMode 恢复终端模式。
func (a *Adapter) restoreMode() {
	if a.rawOldState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), a.rawOldState)
		a.rawOldState = nil
	}
}

// startTerm 使用 term.Terminal 提供行编辑、历史记录和 Tab 补全。
func (a *Adapter) startTerm(ctx stdctx.Context, stopCh <-chan struct{}) error {
	defer a.running.Store(false)
	defer a.restoreMode()

	rw := &readWriter{a.reader, a.writer}
	t := term.NewTerminal(rw, a.prompt)
	t.AutoCompleteCallback = a.makeAutoComplete()

	// 发布行编辑器，使 Send/Notify 等输出走它而不是裸 writer。
	a.mu.Lock()
	a.termOut = t
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.termOut = nil
		a.mu.Unlock()
	}()

	// 读取放到独立 goroutine，使 Stop/ctx 能真正中断等待。
	//
	// t.ReadLine() 与 scanner.Scan() 都不可中断，而它们之间的 select 只在
	// 每次迭代开头轮询一次。此前 Stop() 立即返回 nil，读取循环却仍卡在
	// ReadLine 上并继续持有 raw 模式的 tty：用户随后按下回车，事件会被派发
	// 给一个已经完成拆卸的 Bot（且此处直接调 h(event) 而非 SafeDispatch，
	// 拆卸后的空指针会直接终止进程）。
	readDone := make(chan struct{})
	defer close(readDone)
	lines := readLines(func() (string, error) { return t.ReadLine() }, readDone)

	for {
		select {
		case <-ctx.Done():
			a.restoreMode()
			a.output("\n[Terminal] Context 已取消，正在停止...\n")
			return ctx.Err()
		case <-stopCh:
			a.restoreMode()
			a.output("\n[Terminal] 收到停止请求，正在关闭...\n")
			return nil
		case res := <-lines:
			if res.err != nil {
				if res.err == io.EOF {
					a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 已关闭 (EOF)"))
					a.output("\n[Terminal] 到达 EOF，正在停止...\n")
					return nil
				}
				a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 读取错误: %w", res.err))
				return fmt.Errorf("terminal adapter: 读取错误: %w", res.err)
			}

			line := strings.TrimSpace(res.line)
			if line == "" {
				continue
			}
			if strings.EqualFold(line, "quit") || strings.EqualFold(line, "exit") {
				a.output("[Terminal] 再见！\n")
				return nil
			}
			a.dispatch(NewEvent(line))
		}
	}
}

// lineResult 是一次输入读取的结果。
type lineResult struct {
	line string
	err  error
}

// readLines 在独立 goroutine 中反复调用 read，并把结果送入返回的 channel。
//
// done 关闭后 goroutine 会在下一次发送时退出，避免它长期占住 stdin。
//
// 为什么必须可退出：os.Stdin 上的阻塞读取没有可移植的取消手段，若放任
// goroutine 停在那里，Stop→Start 循环（Bot.Restart）会为同一个 fd 再起一个
// 读取者。两个读取者争抢输入，用户敲下的每一行落到谁手里全凭运气；
// bufio.Scanner 还按 64 KiB 成块读取，陈旧的那个可能一口吞掉远不止一行。
// 同时旧 goroutine 会在只有 1 个缓冲位的 channel 上永久阻塞——每次重启
// 泄漏一个 goroutine 加一个扫描缓冲区。
func readLines(read func() (string, error), done <-chan struct{}) <-chan lineResult {
	ch := make(chan lineResult, 1)
	go func() {
		defer close(ch)
		for {
			line, err := read()
			// 先单独检查 done：若与发送分支放在同一个 select 里，两者同时就绪时
			// Go 会随机挑选，约有一半概率把这一行写进已被弃用的 channel，
			// 然后回到阻塞读取——重启后用户敲下的第一行就此消失，
			// 陈旧的 goroutine 也多活一轮。
			select {
			case <-done:
				return
			default:
			}
			select {
			case ch <- lineResult{line: line, err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

// dispatch 把事件交给已注册的 handler。
//
// 使用 platform.SafeDispatch 隔离 panic：handler 由用户提供，
// 在这里直接调用会让一次 panic 击穿读取循环并终止整个进程。
// 其余适配器均已如此处理，终端适配器此前是唯一的例外。
func (a *Adapter) dispatch(event platform.Event) {
	a.mu.Lock()
	h := a.handler
	a.mu.Unlock()
	if h != nil {
		platform.SafeDispatch(h, event)
	}
}

// startScanner 使用 bufio.Scanner 回退实现（无行编辑/历史/补全，但跨平台可靠）。
func (a *Adapter) startScanner(ctx stdctx.Context, stopCh <-chan struct{}) error {
	defer a.running.Store(false)

	scanner := bufio.NewScanner(a.reader)
	// 与 startTerm 同理：scanner.Scan() 不可中断，必须放到独立 goroutine，
	// 否则 Stop() 返回后循环仍卡在读取上，用户下一次回车会把事件派发给
	// 一个已经拆卸完毕的 Bot。
	readDone := make(chan struct{})
	defer close(readDone)
	lines := readLines(func() (string, error) {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}, readDone)

	_, _ = fmt.Fprint(a.writer, a.prompt)

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(a.writer, "\n[Terminal] Context 已取消，正在停止...")
			return ctx.Err()
		case <-stopCh:
			fmt.Fprintln(a.writer, "\n[Terminal] 收到停止请求，正在关闭...")
			return nil
		case res := <-lines:
			if res.err != nil {
				if res.err == io.EOF {
					a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 已关闭 (EOF)"))
					fmt.Fprintln(a.writer, "\n[Terminal] 到达 EOF，正在停止...")
					return nil
				}
				a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 读取错误: %w", res.err))
				return fmt.Errorf("terminal adapter: 读取错误: %w", res.err)
			}

			line := strings.TrimSpace(res.line)
			if line != "" {
				if strings.EqualFold(line, "quit") || strings.EqualFold(line, "exit") {
					a.output("[Terminal] 再见！\n")
					return nil
				}
				a.dispatch(NewEvent(line))
			}
			_, _ = fmt.Fprint(a.writer, a.prompt)
		}
	}
}

// makeAutoComplete 构造 AutoCompleteCallback，处理 Tab 补全。
//
// 单候选项时内联替换当前编辑行；多候选项时打印列表。
// 依赖 enableVT100 确保 Windows 上 VT100 转义序列可用。
func (a *Adapter) makeAutoComplete() func(string, int, rune) (string, int, bool) {
	return func(line string, pos int, key rune) (string, int, bool) {
		if key != '\t' || a.completionFunc == nil {
			return "", 0, false
		}

		prefix := line[:pos]
		lastSpace := strings.LastIndex(prefix, " ")
		if lastSpace >= 0 {
			prefix = prefix[lastSpace+1:]
		}

		candidates := a.completionFunc(prefix)
		if len(candidates) == 0 {
			return "", 0, false
		}

		// 去重：相同输入 + 相同候选项不再重复输出
		if line == a.lastCompLine && sliceEqual(candidates, a.lastCompCandidates) {
			return line, pos, true
		}
		a.lastCompLine = line
		a.lastCompCandidates = candidates

		if len(candidates) == 1 && candidates[0] != prefix {
			// 单候选项：内联替换（依赖 VT100 擦除行后重绘）
			completion := line[:lastSpace+1] + candidates[0]
			if lastSpace < 0 {
				completion = candidates[0]
			}
			return completion, len(completion), true
		}

		// 多候选项：在下一行输出列表，原 Prompt 行不受影响
		var show bool
		for _, c := range candidates {
			if c != prefix {
				show = true
				break
			}
		}
		if show {
			_, _ = fmt.Fprintf(a.writer, "\r\n%s\n", strings.Join(candidates, "  "))
		}
		return line, pos, true
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a *Adapter) Stop(_ stdctx.Context) error {
	// 关闭动作必须在锁内完成：原先的 select-default + close 组合不是原子操作，
	// 两个 goroutine（例如 API 的 DELETE /platforms/terminal 与 SIGINT 触发的
	// registry.StopAll）可以同时走到 default 分支并双双 close，
	// 触发 "close of closed channel" panic 并终止进程。
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return nil // 幂等
	}
	a.stopped = true
	close(a.stopCh)
	return nil
}

func (a *Adapter) Sender() platform.Sender {
	return a
}

func (a *Adapter) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Markdown:        true,
		Buttons:         false,
		MultiAttachment: false,
		MessageEdit:     true,
		MessageDelete:   true,
		Embeds:          false,
		FileUpload:      false,
		GuildSupport:    false,
		Reactions:       true,
		ThreadReply:     false,
		TypingIndicator: true,
		MentionAll:      false,
		VoiceChannel:    false,
		Caption:         true, // 终端渲染器可同时展示文本与图片
	}
}

func (a *Adapter) IsRunning() bool {
	return a.running.Load()
}

func (a *Adapter) BotID() string {
	return a.botID
}

func (a *Adapter) BotName() string {
	return a.botName
}

// ── platform.HealthDetailer ──────────────────────────────────────────────────

func (a *Adapter) HealthDetail() map[string]any {
	return map[string]any{
		"connection": "terminal",
		"prompt":     a.prompt,
	}
}

// ── 编译期接口断言 ────────────────────────────────────────────────────────────

var (
	_ platform.Adapter            = (*Adapter)(nil)
	_ platform.BotIdentity        = (*Adapter)(nil)
	_ platform.RecoverableAdapter = (*Adapter)(nil)
	_ platform.HealthDetailer     = (*Adapter)(nil)
)

func (a *Adapter) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	// 与其余 Sender 实现保持一致：非法请求（空 Target.ID、
	// URL 与 Data 同时设置等）必须在入口拒绝，而不是静默发出去。
	if err := req.Validate(); err != nil {
		return platform.SendResult{}, err
	}

	content := extractMessageContent(req.Message)
	msgID := fmt.Sprintf("term-msg-%d", a.msgCount.Add(1))

	sent := &SentMessage{
		ID:      msgID,
		Content: content,
		Target:  req.Target,
	}

	a.msgMu.Lock()
	a.messages = append(a.messages, sent)
	a.msgMu.Unlock()

	a.output("[Bot Reply] %s\n", content)

	return platform.SendResult{
		MessageID: msgID,
		Platform:  PlatformID,
	}, nil
}

func (a *Adapter) Edit(_ stdctx.Context, _, messageID string, msg platform.OutboundMessage) error {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()

	for _, m := range a.messages {
		if m.ID == messageID {
			newContent := extractMessageContent(msg)
			m.Content = newContent
			m.Edited = true
			a.output("[Bot Edited] %s\n", newContent)
			return nil
		}
	}
	return fmt.Errorf("terminal adapter: 找不到消息 %s", messageID)
}

func (a *Adapter) Delete(_ stdctx.Context, _, messageID string) error {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()

	for _, m := range a.messages {
		if m.ID == messageID {
			m.Deleted = true
			a.output("[Bot Deleted] 消息 %s 已撤回\n", messageID)
			return nil
		}
	}
	return fmt.Errorf("terminal adapter: 找不到消息 %s", messageID)
}

func (a *Adapter) SendTyping(_ stdctx.Context, _ string) error {
	a.output("[Bot 正在输入...]\n")
	return nil
}

func (a *Adapter) AddReaction(_ stdctx.Context, _, messageID string, emoji platform.Emoji) error {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()

	for _, m := range a.messages {
		if m.ID == messageID {
			m.Reactions = append(m.Reactions, emoji)
			label := emojiLabel(emoji)
			a.output("[Bot Reaction] +%s 消息 %s\n", label, messageID)
			return nil
		}
	}
	return fmt.Errorf("terminal adapter: 找不到消息 %s", messageID)
}

func (a *Adapter) RemoveReaction(_ stdctx.Context, _, messageID string, emoji platform.Emoji) error {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()

	for _, m := range a.messages {
		if m.ID == messageID {
			m.Reactions = removeEmoji(m.Reactions, emoji)
			label := emojiLabel(emoji)
			a.output("[Bot Reaction] -%s 消息 %s\n", label, messageID)
			return nil
		}
	}
	return fmt.Errorf("terminal adapter: 找不到消息 %s", messageID)
}

func (a *Adapter) GetGroupInfo(_ stdctx.Context, groupID string) (platform.GroupInfo, error) {
	return platform.GroupInfo{
		ID:          groupID,
		Name:        "终端模拟群组",
		MemberCount: 1,
		Description: "终端适配器模拟的群组",
	}, nil
}

func (a *Adapter) GetGroupMemberList(_ stdctx.Context, _ string) ([]platform.GroupMemberInfo, error) {
	return []platform.GroupMemberInfo{
		{
			UserID:      DefaultUserID,
			DisplayName: "Terminal User",
			GroupRole:   platform.GroupRoleMember,
		},
	}, nil
}

func (a *Adapter) GetGroupMember(_ stdctx.Context, _, userID string) (platform.GroupMemberInfo, error) {
	if userID == DefaultUserID {
		return platform.GroupMemberInfo{
			UserID:      DefaultUserID,
			DisplayName: "Terminal User",
			GroupRole:   platform.GroupRoleMember,
		}, nil
	}
	return platform.GroupMemberInfo{}, fmt.Errorf("terminal adapter: 找不到用户 %s", userID)
}

func (a *Adapter) GetJoinedGroups(_ stdctx.Context) ([]platform.GroupInfo, error) {
	return []platform.GroupInfo{}, nil
}

func (a *Adapter) GetUserAvatarURL(_ stdctx.Context, userID string) (string, error) {
	return fmt.Sprintf("https://via.placeholder.com/100?text=%s", userID), nil
}

func (a *Adapter) NotifyUser(_ stdctx.Context, userID string, msg platform.OutboundMessage) error {
	content := extractMessageContent(msg)
	a.output("[Bot Notify→%s] %s\n", userID, content)
	return nil
}

func (a *Adapter) NotifyGroup(_ stdctx.Context, groupID string, msg platform.OutboundMessage) error {
	content := extractMessageContent(msg)
	a.output("[Bot Notify→群%s] %s\n", groupID, content)
	return nil
}

func (a *Adapter) DeleteMemberMessage(_ stdctx.Context, groupID, messageID string) error {
	a.output("[AutoModerator] 已删除群 %s 中的消息 %s\n", groupID, messageID)
	return nil
}

func (a *Adapter) MuteAll(_ stdctx.Context, groupID string, mute bool) error {
	status := "已开启"
	if !mute {
		status = "已解除"
	}
	a.output("[AutoModerator] 群 %s 全体禁言 %s\n", groupID, status)
	return nil
}

func (a *Adapter) AcceptGroupInvite(_ stdctx.Context, inviteID string) error {
	a.output("[Invitation] 已接受群组邀请 %s\n", inviteID)
	return nil
}

func (a *Adapter) RejectGroupInvite(_ stdctx.Context, inviteID, reason string) error {
	a.output("[Invitation] 已拒绝群组邀请 %s (原因: %s)\n", inviteID, reason)
	return nil
}

func (a *Adapter) AcceptFriendRequest(_ stdctx.Context, requestID string) error {
	a.output("[Invitation] 已接受好友申请 %s\n", requestID)
	return nil
}

func (a *Adapter) RejectFriendRequest(_ stdctx.Context, requestID, reason string) error {
	a.output("[Invitation] 已拒绝好友申请 %s (原因: %s)\n", requestID, reason)
	return nil
}

func (a *Adapter) KickMember(_ stdctx.Context, groupID, userID string, _ bool) error {
	a.output("[GroupManager] 已将用户 %s 踢出群 %s\n", userID, groupID)
	return nil
}

func (a *Adapter) BanMember(_ stdctx.Context, groupID, userID string, duration time.Duration) error {
	if duration == 0 {
		a.output("[GroupManager] 已解除用户 %s 在群 %s 的禁言\n", userID, groupID)
	} else {
		a.output("[GroupManager] 用户 %s 在群 %s 被禁言 %v\n", userID, groupID, duration)
	}
	return nil
}

func (a *Adapter) SetAdmin(_ stdctx.Context, groupID, userID string, isAdmin bool) error {
	action := "撤销"
	if isAdmin {
		action = "授予"
	}
	a.output("[GroupManager] 已%s用户 %s 在群 %s 的管理员身份\n", action, userID, groupID)
	return nil
}

func (a *Adapter) Messages() []*SentMessage {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()
	result := make([]*SentMessage, len(a.messages))
	copy(result, a.messages)
	return result
}

func (a *Adapter) LastMessage() *SentMessage {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()
	if len(a.messages) == 0 {
		return nil
	}
	return a.messages[len(a.messages)-1]
}

func (a *Adapter) Clear() {
	a.msgMu.Lock()
	a.messages = a.messages[:0]
	a.msgMu.Unlock()
}

func (a *Adapter) SimulateMessage(content string) bool {
	a.mu.Lock()
	h := a.handler
	a.mu.Unlock()

	if h == nil || !a.running.Load() {
		return false
	}
	event := NewEvent(content)
	h(event)
	return true
}

func (a *Adapter) SimulateGroupMessage(content string, groupID string) bool {
	a.mu.Lock()
	h := a.handler
	a.mu.Unlock()

	if h == nil || !a.running.Load() {
		return false
	}
	event := NewGroupEvent(content, groupID)
	h(event)
	return true
}

type readWriter struct {
	r io.Reader
	w io.Writer
}

func (rw *readWriter) Read(p []byte) (int, error) {
	return rw.r.Read(p)
}
func (rw *readWriter) Write(p []byte) (int, error) {
	return rw.w.Write(p)
}

func extractMessageContent(msg platform.OutboundMessage) string {
	var parts []string
	if msg.Text != "" {
		parts = append(parts, msg.Text)
	}
	if msg.Markdown != "" {
		parts = append(parts, msg.Markdown)
	}
	for _, att := range msg.Attachments {
		if att.Name != "" {
			parts = append(parts, fmt.Sprintf("[文件: %s]", att.Name))
		} else if att.URL != "" {
			parts = append(parts, fmt.Sprintf("[链接: %s]", att.URL))
		}
	}
	for _, emb := range msg.Embeds {
		title := emb.Title
		if title == "" {
			title = "(卡片)"
		}
		parts = append(parts, fmt.Sprintf("[卡片: %s]", title))
	}
	return strings.Join(parts, "")
}

func emojiLabel(e platform.Emoji) string {
	if e.Kind == platform.EmojiKindUnicode {
		return e.Value
	}
	if e.ID != "" {
		return fmt.Sprintf("%s(%s)", e.Value, e.ID)
	}
	return e.Value
}

func removeEmoji(list []platform.Emoji, target platform.Emoji) []platform.Emoji {
	result := make([]platform.Emoji, 0, len(list))
	for _, e := range list {
		if e.Kind != target.Kind || e.ID != target.ID || e.Value != target.Value {
			result = append(result, e)
		}
	}
	return result
}
