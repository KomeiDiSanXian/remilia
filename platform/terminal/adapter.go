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
	mu       sync.Mutex
	running  atomic.Bool
	msgMu    sync.Mutex
	messages []*SentMessage
	msgCount atomic.Uint64

	reader     io.Reader
	writer     io.Writer
	prompt     string
	welcomeMsg string
	botID      string
	botName    string

	completionFunc func(string) []string

	lastCompLine       string
	lastCompCandidates []string

	rawOldState *term.State

	handler func(platform.Event)
	stopCh  chan struct{}

	platform.DisconnectNotifier
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
	a.mu.Unlock()

	fmt.Fprint(a.writer, a.welcomeMsg)

	// 检查 stdin 是否为真实终端，尝试设置原始模式
	if a.tryRawMode() {
		return a.startTerm(ctx)
	}
	return a.startScanner(ctx)
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
		fmt.Fprintf(a.writer, "[Terminal] 提示: 终端原始模式不可用 (%v)，方向键和 Tab 补全将不支持\n", err)
		return false
	}
	a.rawOldState = oldState

	// Windows 上启用 VT100 转义序列支持（refreshLine 的擦除/重绘需要）
	enableVT100(os.Stdin.Fd())

	return true
}

// restoreMode 恢复终端模式。
func (a *Adapter) restoreMode() {
	if a.rawOldState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), a.rawOldState)
		a.rawOldState = nil
	}
}

// startTerm 使用 term.Terminal 提供行编辑、历史记录和 Tab 补全。
func (a *Adapter) startTerm(ctx stdctx.Context) error {
	defer a.running.Store(false)
	defer a.restoreMode()

	rw := &readWriter{a.reader, a.writer}
	t := term.NewTerminal(rw, a.prompt)
	t.AutoCompleteCallback = a.makeAutoComplete()

	for {
		select {
		case <-ctx.Done():
			a.restoreMode()
			fmt.Fprintln(a.writer, "\n[Terminal] Context 已取消，正在停止...")
			return ctx.Err()
		case <-a.stopCh:
			a.restoreMode()
			fmt.Fprintln(a.writer, "\n[Terminal] 收到停止请求，正在关闭...")
			return nil
		default:
		}

		line, err := t.ReadLine()
		if err != nil {
			if err == io.EOF {
				a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 已关闭 (EOF)"))
				fmt.Fprintln(a.writer, "\n[Terminal] 到达 EOF，正在停止...")
				return nil
			}
			a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 读取错误: %w", err))
			return fmt.Errorf("terminal adapter: 读取错误: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "quit") || strings.EqualFold(line, "exit") {
			fmt.Fprintln(a.writer, "[Terminal] 再见！")
			return nil
		}

		event := NewEvent(line)
		a.mu.Lock()
		h := a.handler
		a.mu.Unlock()
		if h != nil {
			h(event)
		}
	}
}

// startScanner 使用 bufio.Scanner 回退实现（无行编辑/历史/补全，但跨平台可靠）。
func (a *Adapter) startScanner(ctx stdctx.Context) error {
	defer a.running.Store(false)

	scanner := bufio.NewScanner(a.reader)
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(a.writer, "\n[Terminal] Context 已取消，正在停止...")
			return ctx.Err()
		case <-a.stopCh:
			fmt.Fprintln(a.writer, "\n[Terminal] 收到停止请求，正在关闭...")
			return nil
		default:
		}

		fmt.Fprint(a.writer, a.prompt)

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 读取错误: %w", err))
				return fmt.Errorf("terminal adapter: 读取错误: %w", err)
			}
			a.NotifyDisconnect(fmt.Errorf("terminal adapter: stdin 已关闭 (EOF)"))
			fmt.Fprintln(a.writer, "\n[Terminal] 到达 EOF，正在停止...")
			return nil
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.EqualFold(line, "quit") || strings.EqualFold(line, "exit") {
			fmt.Fprintln(a.writer, "[Terminal] 再见！")
			return nil
		}

		event := NewEvent(line)
		a.mu.Lock()
		h := a.handler
		a.mu.Unlock()
		if h != nil {
			h(event)
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
			fmt.Fprintf(a.writer, "\r\n%s\n", strings.Join(candidates, "  "))
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
	select {
	case <-a.stopCh:
	default:
		close(a.stopCh)
	}
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

	fmt.Fprintf(a.writer, "[Bot Reply] %s\n", content)

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
			fmt.Fprintf(a.writer, "[Bot Edited] %s\n", newContent)
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
			fmt.Fprintf(a.writer, "[Bot Deleted] 消息 %s 已撤回\n", messageID)
			return nil
		}
	}
	return fmt.Errorf("terminal adapter: 找不到消息 %s", messageID)
}

func (a *Adapter) SendTyping(_ stdctx.Context, _ string) error {
	fmt.Fprintln(a.writer, "[Bot 正在输入...]")
	return nil
}

func (a *Adapter) AddReaction(_ stdctx.Context, _, messageID string, emoji platform.Emoji) error {
	a.msgMu.Lock()
	defer a.msgMu.Unlock()

	for _, m := range a.messages {
		if m.ID == messageID {
			m.Reactions = append(m.Reactions, emoji)
			label := emojiLabel(emoji)
			fmt.Fprintf(a.writer, "[Bot Reaction] +%s 消息 %s\n", label, messageID)
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
			fmt.Fprintf(a.writer, "[Bot Reaction] -%s 消息 %s\n", label, messageID)
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
	fmt.Fprintf(a.writer, "[Bot Notify→%s] %s\n", userID, content)
	return nil
}

func (a *Adapter) NotifyGroup(_ stdctx.Context, groupID string, msg platform.OutboundMessage) error {
	content := extractMessageContent(msg)
	fmt.Fprintf(a.writer, "[Bot Notify→群%s] %s\n", groupID, content)
	return nil
}

func (a *Adapter) DeleteMemberMessage(_ stdctx.Context, groupID, messageID string) error {
	fmt.Fprintf(a.writer, "[AutoModerator] 已删除群 %s 中的消息 %s\n", groupID, messageID)
	return nil
}

func (a *Adapter) MuteAll(_ stdctx.Context, groupID string, mute bool) error {
	status := "已开启"
	if !mute {
		status = "已解除"
	}
	fmt.Fprintf(a.writer, "[AutoModerator] 群 %s 全体禁言 %s\n", groupID, status)
	return nil
}

func (a *Adapter) AcceptGroupInvite(_ stdctx.Context, inviteID string) error {
	fmt.Fprintf(a.writer, "[Invitation] 已接受群组邀请 %s\n", inviteID)
	return nil
}

func (a *Adapter) RejectGroupInvite(_ stdctx.Context, inviteID, reason string) error {
	fmt.Fprintf(a.writer, "[Invitation] 已拒绝群组邀请 %s (原因: %s)\n", inviteID, reason)
	return nil
}

func (a *Adapter) AcceptFriendRequest(_ stdctx.Context, requestID string) error {
	fmt.Fprintf(a.writer, "[Invitation] 已接受好友申请 %s\n", requestID)
	return nil
}

func (a *Adapter) RejectFriendRequest(_ stdctx.Context, requestID, reason string) error {
	fmt.Fprintf(a.writer, "[Invitation] 已拒绝好友申请 %s (原因: %s)\n", requestID, reason)
	return nil
}

func (a *Adapter) KickMember(_ stdctx.Context, groupID, userID string, _ bool) error {
	fmt.Fprintf(a.writer, "[GroupManager] 已将用户 %s 踢出群 %s\n", userID, groupID)
	return nil
}

func (a *Adapter) BanMember(_ stdctx.Context, groupID, userID string, duration time.Duration) error {
	if duration == 0 {
		fmt.Fprintf(a.writer, "[GroupManager] 已解除用户 %s 在群 %s 的禁言\n", userID, groupID)
	} else {
		fmt.Fprintf(a.writer, "[GroupManager] 用户 %s 在群 %s 被禁言 %v\n", userID, groupID, duration)
	}
	return nil
}

func (a *Adapter) SetAdmin(_ stdctx.Context, groupID, userID string, isAdmin bool) error {
	action := "撤销"
	if isAdmin {
		action = "授予"
	}
	fmt.Fprintf(a.writer, "[GroupManager] 已%s用户 %s 在群 %s 的管理员身份\n", action, userID, groupID)
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
