package discord

import (
	"regexp"
	"strings"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/bwmarrin/discordgo"
)

// segments.go — Discord Message → 统一消息段映射
//
// 段是跨平台唯一真相源：Content()/Attachments() 均由本映射结果派生。
// Discord 无实体 offset 信息，但 mention 在正文中的位置可用
// "<@id> / <@!id> / <@&role> / <#channel>" 占位符精确还原（§5 已注明）。

// mentionTokenRe 匹配 Discord 的四种 mention 占位符：
//   - <@123>    用户 mention
//   - <@!123>   用户 mention（nickname 形式）
//   - <@&123>   角色 mention
//   - <#123>    频道 mention
var mentionTokenRe = regexp.MustCompile(`<@!?(\d+)>|<@&(\d+)>|<#(\d+)>`)

// buildDiscordSegments 将 Discord Message 映射为保序统一段。
//
// 顺序：reply 段（MessageReference 前置）→ 文本/at 交错段 → 媒体段。
func buildDiscordSegments(m *discordgo.Message) []platform.Segment {
	if m == nil {
		return nil
	}
	var segs []platform.Segment
	if m.MessageReference != nil && m.MessageReference.MessageID != "" {
		segs = append(segs, platform.Segment{Type: platform.SegmentReply, ReplyToID: m.MessageReference.MessageID})
	}
	segs = append(segs, splitDiscordMentions(m.Content, m.Mentions)...)
	segs = append(segs, attachmentSegments(m.Attachments)...)
	return segs
}

// splitDiscordMentions 按 mention 占位符把正文切成 text/at 交错段。
//
// 用户 mention（<@id> / <@!id>）→ SegmentAt（DisplayName 从 Mentions 数组查表）；
// 角色/频道 mention → SegmentUnknown + Extra（平台原生 token，Content 剥离）。
func splitDiscordMentions(content string, mentions []*discordgo.User) []platform.Segment {
	if content == "" {
		return nil
	}
	nameByID := make(map[string]string, len(mentions))
	for _, u := range mentions {
		if u == nil {
			continue
		}
		name := u.GlobalName
		if name == "" {
			name = u.Username
		}
		nameByID[u.ID] = name
	}

	var out []platform.Segment
	pos := 0
	for _, loc := range mentionTokenRe.FindAllStringIndex(content, -1) {
		tok := content[loc[0]:loc[1]]
		if loc[0] > pos {
			out = append(out, platform.Segment{Type: platform.SegmentText, Text: content[pos:loc[0]]})
		}
		switch {
		case strings.HasPrefix(tok, "<@"):
			uid := strings.TrimPrefix(tok, "<@")
			uid = strings.TrimPrefix(uid, "!")
			uid = strings.TrimSuffix(uid, ">")
			out = append(out, platform.Segment{Type: platform.SegmentAt, UserID: uid, Text: nameByID[uid]})
		default: // <@&role> / <#channel>
			out = append(out, platform.Segment{
				Type:  platform.SegmentUnknown,
				Extra: map[string]any{"type": "mention", "raw": tok},
			})
		}
		pos = loc[1]
	}
	if pos < len(content) {
		out = append(out, platform.Segment{Type: platform.SegmentText, Text: content[pos:]})
	}
	return out
}

// attachmentSegments 将 Discord 附件按 MIME 分类映射为媒体段（保序）。
func attachmentSegments(atts []*discordgo.MessageAttachment) []platform.Segment {
	var segs []platform.Segment
	for _, a := range atts {
		if a == nil {
			continue
		}
		att := platform.Attachment{
			URL:      a.URL,
			MimeType: a.ContentType,
			Name:     a.Filename,
			Size:     a.Size,
			Width:    a.Width,
			Height:   a.Height,
		}
		var t platform.SegmentType
		switch {
		case strings.HasPrefix(a.ContentType, "image/"):
			t = platform.SegmentImage
		case strings.HasPrefix(a.ContentType, "audio/"):
			t = platform.SegmentAudio
		case strings.HasPrefix(a.ContentType, "video/"):
			t = platform.SegmentVideo
		default:
			t = platform.SegmentFile
		}
		segs = append(segs, platform.Segment{Type: t, Attachment: att})
	}
	return segs
}
