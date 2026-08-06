package discord

import (
	"bytes"
	stdctx "context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
)

// interactionTTL is how long we keep a *discordgo.Interaction in memory.
// Discord interaction tokens are valid for 15 minutes.
const interactionTTL = 15 * time.Minute

// pendingInteraction holds a cached Discord interaction alongside metadata
// that tracks whether the first response has already been sent.
type pendingInteraction struct {
	interaction *discordgo.Interaction
	responded   bool
	createdAt   time.Time
}

// ────────────────────────────────────────────────────────────────────────────
// discordSender
// ────────────────────────────────────────────────────────────────────────────

// discordSender implements platform.Sender and the optional extension interfaces:
//   - platform.MessageEditor   (Edit)
//   - platform.MessageDeleter  (Delete)
//   - platform.ReactionSender  (AddReaction, RemoveReaction)
//   - platform.TypingNotifier  (SendTyping)
type discordSender struct {
	session *discordgo.Session

	intMu   sync.Mutex
	pending map[string]*pendingInteraction // key: interaction ID

	// cleanupMu 保护 stopCh / stopped，使 cleanupLoop 可以随 Start/Stop 反复启停。
	cleanupMu sync.Mutex
	stopCh    chan struct{} // closing signals cleanupLoop to exit
	stopped   bool          // true 表示当前没有在运行的 cleanupLoop
}

// newSender creates a discordSender wrapping the given discordgo session.
func newSender(session *discordgo.Session) *discordSender {
	s := &discordSender{
		session: session,
		pending: make(map[string]*pendingInteraction),
		stopCh:  make(chan struct{}),
	}
	go s.cleanupLoop(s.stopCh)
	return s
}

// PlatformAPI 实现 platform.APIProvider，返回 Discord 原生 SDK 会话，
// 调用方可断言 *discordgo.Session 访问 Discord 全部 REST API。
func (s *discordSender) PlatformAPI() any { return s.session }

// 编译期接口实现检查。
var _ platform.APIProvider = (*discordSender)(nil)

// startCleanup 在 cleanupLoop 已停止时重新启动它。幂等。
//
// 适配器支持 Stop → Start 的重启循环（见 GatewayAdapter.Start 的文档），
// 而 stopCleanup 会永久终止清理协程。若不能重新启动，重启后
// storeInteraction 仍在不断写入 s.pending，却再无任何东西回收，
// 于是把"一个闲置协程"换成了"无上限的内存泄漏"。
func (s *discordSender) startCleanup() {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if !s.stopped {
		return // 已在运行
	}
	s.stopCh = make(chan struct{})
	s.stopped = false
	go s.cleanupLoop(s.stopCh)
}

// cleanupLoop periodically removes expired interaction entries.
// Exits when the stop channel it was started with is closed.
func (s *discordSender) cleanupLoop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.intMu.Lock()
			now := time.Now()
			for id, p := range s.pending {
				if now.Sub(p.createdAt) > interactionTTL {
					delete(s.pending, id)
				}
			}
			s.intMu.Unlock()
		case <-stopCh:
			return
		}
	}
}

// stopCleanup signals cleanupLoop to exit. Idempotent.
//
// 与 startCleanup 配对：Start 时重新拉起，Stop 时停掉，避免
// "停一次就永久失去 GC" 或 "每次 Start 泄漏一个协程" 两种极端。
func (s *discordSender) stopCleanup() {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopCh)
}

// storeInteraction registers a Discord interaction so the sender can dispatch
// responses via the Interactions API rather than the channel messages API.
//
// Called by GatewayAdapter when an INTERACTION_CREATE event is received.
func (s *discordSender) storeInteraction(i *discordgo.Interaction) {
	if i == nil {
		return
	}
	s.intMu.Lock()
	s.pending[i.ID] = &pendingInteraction{
		interaction: i,
		responded:   false,
		createdAt:   time.Now(),
	}
	s.intMu.Unlock()
}

// ────────────────────────────────────────────────────────────────────────────
// platform.Sender
// ────────────────────────────────────────────────────────────────────────────

// Send dispatches an OutboundMessage via the Discord REST API.
//
// Routing:
//   - req.Target.Tokens[TokenInteractionID] is set → interaction response / follow-up
//   - Otherwise → regular channel message
//
// SendResult.MessageID is the Discord snowflake message ID.
func (s *discordSender) Send(_ stdctx.Context, req platform.SendRequest) (platform.SendResult, error) {
	if s.session == nil {
		return platform.SendResult{}, fmt.Errorf("discord sender: session is nil")
	}

	chat := req.Target
	if chat.ID == "" {
		return platform.SendResult{}, errutil.ErrNoChatInfo
	}

	msg := req.Message
	extra := extractExtra(msg)

	// Interaction response path
	interactionID := chat.Tokens[TokenInteractionID]
	if interactionID != "" {
		return platform.SendResult{Platform: "discord"}, s.sendInteractionResponse(interactionID, msg, extra)
	}

	// Regular channel message
	ms := buildMessageSend(msg, extra)
	if msg.IsEmpty() {
		return platform.SendResult{}, errutil.ErrEmptyMessage
	}
	sent, err := s.session.ChannelMessageSendComplex(chat.ID, ms)
	if err != nil {
		return platform.SendResult{}, platform.NewSendError(
			platform.SendErrPlatform, "discord", chat.ID,
			err.Error(), 0, err,
		)
	}
	result := platform.SendResult{
		Platform:  "discord",
		MessageID: sent.ID,
		Timestamp: sent.Timestamp,
	}
	return result, nil
}

// sendInteractionResponse sends the first response or a follow-up for an interaction.
func (s *discordSender) sendInteractionResponse(interactionID string, msg platform.OutboundMessage, extra MessageExtra) error {
	s.intMu.Lock()
	p, ok := s.pending[interactionID]
	if !ok {
		s.intMu.Unlock()
		return fmt.Errorf("discord sender: no pending interaction for ID %q (expired or unknown)", interactionID)
	}

	if !p.responded {
		// First response: use InteractionRespond (type 4 = message with source)
		//
		// 乐观置位 + 失败回滚：置位必须在锁内完成，否则两个并发 Send 会同时
		// 看到 !responded 并各发一次 type-4 响应，第二次被 Discord 以
		// 40060（already acknowledged）拒绝。
		p.responded = true
		i := p.interaction
		s.intMu.Unlock()

		if err := s.session.InteractionRespond(i, buildInteractionResponse(msg, extra)); err != nil {
			// 首响失败必须回滚：Discord 侧该 interaction 仍未被确认，而
			// followup 走 /webhooks/{app}/{token}，要求先确认过。
			// 不回滚的话，一次瞬时 5xx 会让这个 interaction 在其 15 分钟
			// 生命周期内彻底无法再回复——任何重试都必然 404。
			s.intMu.Lock()
			p.responded = false
			s.intMu.Unlock()
			return err
		}
		return nil
	}
	// Subsequent responses: use follow-up messages
	i := p.interaction
	s.intMu.Unlock()

	_, err := s.session.FollowupMessageCreate(i, true, buildFollowupParams(msg, extra))
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// platform.MessageEditor
// ────────────────────────────────────────────────────────────────────────────

// Edit implements platform.MessageEditor.
func (s *discordSender) Edit(_ stdctx.Context, chatID, messageID string, msg platform.OutboundMessage) error {
	if chatID == "" || messageID == "" {
		return fmt.Errorf("discord sender: chatID and messageID must not be empty for Edit")
	}
	extra := extractExtra(msg)
	me := buildMessageEdit(chatID, messageID, msg, extra)
	_, err := s.session.ChannelMessageEditComplex(me)
	return err
}

// ────────────────────────────────────────────────────────────────────────────
// platform.MessageDeleter
// ────────────────────────────────────────────────────────────────────────────

// Delete implements platform.MessageDeleter.
func (s *discordSender) Delete(_ stdctx.Context, chatID, messageID string) error {
	if chatID == "" || messageID == "" {
		return fmt.Errorf("discord sender: chatID and messageID must not be empty for Delete")
	}
	return s.session.ChannelMessageDelete(chatID, messageID)
}

// ────────────────────────────────────────────────────────────────────────────
// platform.ReactionSender
// ────────────────────────────────────────────────────────────────────────────

// AddReaction implements platform.ReactionSender.
//
// emoji is mapped to the Discord wire format:
//   - EmojiKindUnicode: emoji.Value (e.g. "👍")
//   - EmojiKindCustom:  "name:id" format (e.g. "myEmoji:123456789")
//   - EmojiKindSystem:  emoji.Value (fallback)
func (s *discordSender) AddReaction(_ stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	return s.session.MessageReactionAdd(chatID, messageID, emojiToDiscord(emoji))
}

// RemoveReaction implements platform.ReactionSender.
//
// Removes the bot's own reaction (@me).
func (s *discordSender) RemoveReaction(_ stdctx.Context, chatID, messageID string, emoji platform.Emoji) error {
	return s.session.MessageReactionRemove(chatID, messageID, emojiToDiscord(emoji), "@me")
}

// emojiToDiscord converts a platform.Emoji to the Discord wire format.
//
// Discord expects:
//   - Standard Unicode emoji as-is (e.g. "👍")
//   - Custom guild emoji as "name:id" (e.g. "myEmoji:123456789")
func emojiToDiscord(e platform.Emoji) string {
	if e.Kind == platform.EmojiKindCustom && e.ID != "" {
		name := e.Value
		if name == "" {
			return e.ID
		}
		return name + ":" + e.ID
	}
	return e.Value
}

// ────────────────────────────────────────────────────────────────────────────
// platform.TypingNotifier
// ────────────────────────────────────────────────────────────────────────────

// SendTyping implements platform.TypingNotifier.
//
// Triggers the "Bot is typing..." indicator in the channel for ~10 seconds.
func (s *discordSender) SendTyping(_ stdctx.Context, chatID string) error {
	return s.session.ChannelTyping(chatID)
}

// ────────────────────────────────────────────────────────────────────────────
// Message builders
// ────────────────────────────────────────────────────────────────────────────

// buildMessageSend converts a platform.OutboundMessage to *discordgo.MessageSend.
func buildMessageSend(msg platform.OutboundMessage, extra MessageExtra) *discordgo.MessageSend {
	ms := &discordgo.MessageSend{TTS: extra.TTS}

	// Content: use Markdown if available (Discord renders Markdown natively).
	if msg.Markdown != "" {
		ms.Content = msg.Markdown
	} else {
		ms.Content = msg.Text
	}

	// Prepend @mentions
	if len(msg.Mentions) > 0 {
		prefix := buildMentionPrefix(msg.Mentions)
		ms.Content = prefix + ms.Content
	}

	// Reply reference
	if msg.ReplyToID != "" {
		ms.Reference = &discordgo.MessageReference{MessageID: msg.ReplyToID}
	}

	// Embeds
	if len(msg.Embeds) > 0 {
		ms.Embeds = convertEmbeds(msg.Embeds)
	}

	// Interactive components (buttons)
	if len(msg.Buttons) > 0 {
		ms.Components = convertButtons(msg.Buttons)
	}

	// File attachments
	if len(msg.Attachments) > 0 {
		ms.Files = convertFiles(msg.Attachments)
	}

	// Allowed mentions
	if extra.AllowedMentions != nil {
		ms.AllowedMentions = buildAllowedMentions(extra.AllowedMentions)
	}

	return ms
}

// buildMessageEdit converts a platform.OutboundMessage to *discordgo.MessageEdit.
func buildMessageEdit(channelID, messageID string, msg platform.OutboundMessage, extra MessageExtra) *discordgo.MessageEdit {
	content := msg.Markdown
	if content == "" {
		content = msg.Text
	}

	me := &discordgo.MessageEdit{
		Channel: channelID,
		ID:      messageID,
		Content: &content,
	}

	if len(msg.Embeds) > 0 {
		me.Embeds = new(convertEmbeds(msg.Embeds))
	}
	if len(msg.Buttons) > 0 {
		me.Components = new(convertButtons(msg.Buttons))
	}
	if extra.AllowedMentions != nil {
		me.AllowedMentions = buildAllowedMentions(extra.AllowedMentions)
	}
	return me
}

// buildInteractionResponse builds the first interaction response (type 4).
func buildInteractionResponse(msg platform.OutboundMessage, extra MessageExtra) *discordgo.InteractionResponse {
	content := msg.Markdown
	if content == "" {
		content = msg.Text
	}
	if len(msg.Mentions) > 0 {
		content = buildMentionPrefix(msg.Mentions) + content
	}

	data := &discordgo.InteractionResponseData{
		Content: content,
		TTS:     extra.TTS,
	}

	if extra.Ephemeral {
		data.Flags |= discordgo.MessageFlagsEphemeral
	}
	if extra.SuppressEmbeds {
		data.Flags |= discordgo.MessageFlagsSuppressEmbeds
	}
	if len(msg.Embeds) > 0 {
		data.Embeds = convertEmbeds(msg.Embeds)
	}
	if len(msg.Buttons) > 0 {
		data.Components = convertButtons(msg.Buttons)
	}
	if len(msg.Attachments) > 0 {
		data.Files = convertFiles(msg.Attachments)
	}
	if extra.AllowedMentions != nil {
		data.AllowedMentions = buildAllowedMentions(extra.AllowedMentions)
	}

	return &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: data,
	}
}

// buildFollowupParams builds WebhookParams for a follow-up message.
func buildFollowupParams(msg platform.OutboundMessage, extra MessageExtra) *discordgo.WebhookParams {
	content := msg.Markdown
	if content == "" {
		content = msg.Text
	}
	if len(msg.Mentions) > 0 {
		content = buildMentionPrefix(msg.Mentions) + content
	}

	p := &discordgo.WebhookParams{
		Content: content,
		TTS:     extra.TTS,
	}

	if extra.Ephemeral {
		p.Flags |= discordgo.MessageFlagsEphemeral
	}
	if extra.SuppressEmbeds {
		p.Flags |= discordgo.MessageFlagsSuppressEmbeds
	}
	if len(msg.Embeds) > 0 {
		p.Embeds = convertEmbeds(msg.Embeds)
	}
	if len(msg.Buttons) > 0 {
		p.Components = convertButtons(msg.Buttons)
	}
	if len(msg.Attachments) > 0 {
		p.Files = convertFiles(msg.Attachments)
	}
	if extra.AllowedMentions != nil {
		p.AllowedMentions = buildAllowedMentions(extra.AllowedMentions)
	}
	return p
}

// ────────────────────────────────────────────────────────────────────────────
// Conversion helpers
// ────────────────────────────────────────────────────────────────────────────

// buildMentionPrefix builds a space-free @mention prefix string.
func buildMentionPrefix(userIDs []string) string {
	var b strings.Builder
	for _, uid := range userIDs {
		fmt.Fprintf(&b, "<@%s>", uid)
	}
	return b.String()
}

// convertEmbeds converts []platform.Embed to []*discordgo.MessageEmbed.
func convertEmbeds(embeds []platform.Embed) []*discordgo.MessageEmbed {
	result := make([]*discordgo.MessageEmbed, 0, len(embeds))
	for _, e := range embeds {
		de := &discordgo.MessageEmbed{
			Title:       e.Title,
			Description: e.Description,
			URL:         e.URL,
			Color:       int(e.Color),
		}
		if !e.Timestamp.IsZero() {
			de.Timestamp = e.Timestamp.Format(time.RFC3339)
		}
		if e.FooterText != "" {
			de.Footer = &discordgo.MessageEmbedFooter{Text: e.FooterText}
		}
		if e.ImageURL != "" {
			de.Image = &discordgo.MessageEmbedImage{URL: e.ImageURL}
		}
		if e.ThumbnailURL != "" {
			de.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: e.ThumbnailURL}
		}
		for _, f := range e.Fields {
			de.Fields = append(de.Fields, &discordgo.MessageEmbedField{
				Name:   f.Name,
				Value:  f.Value,
				Inline: f.Inline,
			})
		}
		result = append(result, de)
	}
	return result
}

// convertButtons groups []platform.Button into Discord ActionsRow components.
//
// Buttons with Row == ButtonRowAuto (0) each get their own row.
// Buttons with the same Row value (1–5) are grouped into the same ActionsRow.
// Discord limits: 5 rows max, 5 buttons per row; excess buttons are dropped.
func convertButtons(buttons []platform.Button) []discordgo.MessageComponent {
	type rowEntry struct {
		row      int
		children []discordgo.MessageComponent
	}

	var rows []rowEntry
	rowIdx := make(map[int]int) // row value → index in rows

	for _, btn := range buttons {
		db := discordgo.Button{
			Label:    btn.Label,
			CustomID: btn.ID,
			URL:      btn.URL,
			Disabled: btn.Disabled,
		}
		switch btn.Style {
		case platform.ButtonStylePrimary:
			db.Style = discordgo.PrimaryButton
		case platform.ButtonStyleSecondary:
			db.Style = discordgo.SecondaryButton
		case platform.ButtonStyleDanger:
			db.Style = discordgo.DangerButton
		case platform.ButtonStyleLink:
			db.Style = discordgo.LinkButton
		default:
			db.Style = discordgo.SecondaryButton
		}
		if btn.Emoji != "" {
			emoji := discordgo.ComponentEmoji{Name: btn.Emoji}
			db.Emoji = &emoji
		}

		row := btn.Row
		if row <= 0 || row > 5 {
			// ButtonRowAuto: each button gets its own row
			rows = append(rows, rowEntry{row: -(len(rows) + 1), children: []discordgo.MessageComponent{db}})
			continue
		}

		if idx, ok := rowIdx[row]; ok {
			if len(rows[idx].children) < 5 { // max 5 buttons per row
				rows[idx].children = append(rows[idx].children, db)
			}
		} else {
			rowIdx[row] = len(rows)
			rows = append(rows, rowEntry{row: row, children: []discordgo.MessageComponent{db}})
		}
	}

	result := make([]discordgo.MessageComponent, 0, len(rows))
	for i, r := range rows {
		if i >= 5 { // max 5 rows
			break
		}
		result = append(result, discordgo.ActionsRow{Components: r.children})
	}
	return result
}

// convertFiles converts []platform.Attachment to []*discordgo.File.
//
// URL-only attachments are skipped because Discord's file upload API requires
// binary data. Use Attachment.Data for direct uploads, or embed the URL in
// message content / embeds for link-based media.
func convertFiles(atts []platform.Attachment) []*discordgo.File {
	result := make([]*discordgo.File, 0, len(atts))
	for _, att := range atts {
		if len(att.Data) == 0 {
			continue // skip URL-only; no binary data to upload
		}
		name := att.Name
		if name == "" {
			name = "attachment"
		}
		result = append(result, &discordgo.File{
			Name:        name,
			ContentType: att.MimeType,
			Reader:      bytes.NewReader(att.Data),
		})
	}
	return result
}

// buildAllowedMentions converts *AllowedMentions to *discordgo.MessageAllowedMentions.
func buildAllowedMentions(a *AllowedMentions) *discordgo.MessageAllowedMentions {
	if a == nil {
		return nil
	}
	parse := make([]discordgo.AllowedMentionType, 0, len(a.Parse))
	for _, p := range a.Parse {
		parse = append(parse, discordgo.AllowedMentionType(p))
	}
	return &discordgo.MessageAllowedMentions{
		Parse:       parse,
		Roles:       a.Roles,
		Users:       a.Users,
		RepliedUser: a.RepliedUser,
	}
}
