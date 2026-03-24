package help

import (
	"fmt"
	"image/color"
	"strings"
	"time"
	"unicode"

	"github.com/KomeiDiSanXian/remilia/infra/textimage"
)

const helpImageWidth = 700

// renderHelpImage converts help text into a styled PNG card image.
//
// Layout rules applied to the raw text:
//   - First non-empty line → large gold title
//   - After the title a thin divider is always inserted automatically
//   - The first "====…" separator in the source text is skipped (redundant with the auto-divider)
//   - Subsequent "====…" separators become thin rule lines; content after the 2nd one is treated as a footer
//   - Lines matching 【category】 become highlighted category headers
//   - Everything else is body text
//
// A watermark bar is always appended at the bottom, showing the generation
// timestamp and how long the rendering took.
//
// Returns PNG bytes or an error.  Callers should fall back to plain text on error.
func renderHelpImage(helpText string) ([]byte, error) {
	// ── Timing – must be the very first statement ─────────────────────────
	genAt := time.Now()

	// ── Background gradient ───────────────────────────────────────────────
	bg := textimage.LinearGradient(helpImageWidth, 900, 150,
		textimage.Stop(0.0, color.RGBA{R: 16, G: 14, B: 42, A: 255}),
		textimage.Stop(0.5, color.RGBA{R: 18, G: 27, B: 62, A: 255}),
		textimage.Stop(1.0, color.RGBA{R: 12, G: 38, B: 74, A: 255}),
	)

	canvas, err := textimage.NewCanvas(helpImageWidth,
		textimage.WithCJKFont(),
		textimage.WithBgImage(bg, textimage.BgFitFill),
		textimage.WithFontColor(color.RGBA{R: 215, G: 225, B: 245, A: 255}),
		textimage.WithLineHeight(1.65),
		textimage.WithPadding(28, 12),
	)
	if err != nil {
		return nil, fmt.Errorf("help image canvas: %w", err)
	}

	// ── Parse text into typed sections ────────────────────────────────────
	const (
		kindTitle    = iota // large gold title
		kindSep             // "====…" → thin rule
		kindCategory        // 【...】 → highlighted header
		kindBody            // normal content
		kindFooter          // content after 2nd separator
	)
	type section struct {
		kind int
		text string
	}

	rawLines := strings.Split(strings.TrimRight(helpText, "\n "), "\n")
	sections := make([]section, 0, len(rawLines))
	sepCount := 0
	inFooter := false

	for i, raw := range rawLines {
		trimmed := strings.TrimSpace(raw)

		// First line is always the title (even if empty)
		if i == 0 {
			sections = append(sections, section{kindTitle, trimmed})
			continue
		}

		// Separator: a run of "=" characters ≥ 8
		if len(trimmed) >= 8 && strings.Count(trimmed, "=") == len(trimmed) {
			sepCount++
			if sepCount >= 2 {
				inFooter = true
			}
			sections = append(sections, section{kindSep, ""})
			continue
		}

		// Category header: 【…】
		if !inFooter && strings.HasPrefix(trimmed, "【") && strings.HasSuffix(trimmed, "】") {
			sections = append(sections, section{kindCategory, trimmed})
			continue
		}

		if inFooter {
			sections = append(sections, section{kindFooter, raw})
		} else {
			sections = append(sections, section{kindBody, raw})
		}
	}

	// ── Render sections ───────────────────────────────────────────────────
	canvas.AddSpacer(24)

	const dividerLine = "─────────────────────────────────────────────────────"

	var bodyBuf strings.Builder
	var footerBuf strings.Builder
	skipFirstSep := false // the first ====== is always redundant with the auto-divider

	flushBody := func() error {
		text := strings.TrimRight(bodyBuf.String(), "\n")
		bodyBuf.Reset()
		if text == "" {
			return nil
		}
		return canvas.AddText(cleanForImage(text),
			textimage.WithFontSize(14),
			textimage.WithPadding(32, 4),
		)
	}

	for _, sec := range sections {
		switch sec.kind {

		case kindTitle:
			// Large, centred, gold title
			if err := canvas.AddText(cleanForImage(sec.text),
				textimage.WithFontSize(22),
				textimage.WithAlign(textimage.AlignCenter),
				textimage.WithFontColor(color.RGBA{R: 255, G: 215, B: 55, A: 255}),
				textimage.WithTextShadow(color.RGBA{A: 170}, 1, 2, 4),
				textimage.WithPadding(28, 18),
			); err != nil {
				return nil, err
			}
			// Auto-insert a thin divider right after the title
			canvas.AddSpacer(4)
			if err := canvas.AddText(dividerLine,
				textimage.WithFontSize(10),
				textimage.WithAlign(textimage.AlignCenter),
				textimage.WithFontColor(color.RGBA{R: 50, G: 78, B: 138, A: 220}),
				textimage.WithPadding(28, 2),
			); err != nil {
				return nil, err
			}
			canvas.AddSpacer(8)
			// The first ====== in the source text duplicates this divider — skip it.
			skipFirstSep = true

		case kindSep:
			if skipFirstSep {
				skipFirstSep = false
				continue // already drew the divider after the title
			}
			if err := flushBody(); err != nil {
				return nil, err
			}
			canvas.AddSpacer(6)
			if err := canvas.AddText(dividerLine,
				textimage.WithFontSize(10),
				textimage.WithAlign(textimage.AlignCenter),
				textimage.WithFontColor(color.RGBA{R: 50, G: 78, B: 138, A: 220}),
				textimage.WithPadding(28, 2),
			); err != nil {
				return nil, err
			}
			canvas.AddSpacer(8)

		case kindCategory:
			if err := flushBody(); err != nil {
				return nil, err
			}
			canvas.AddSpacer(8)
			if err := canvas.AddText(sec.text,
				textimage.WithFontSize(15),
				textimage.WithFontColor(color.RGBA{R: 95, G: 215, B: 220, A: 255}),
				textimage.WithTextBackdrop(
					color.NRGBA{R: 30, G: 70, B: 145, A: 80},
					0,
				),
				textimage.WithTextBackdropPadding(10, 4),
				textimage.WithTextBackdropShape(textimage.BackdropShapeRounded, 6),
				textimage.WithPadding(28, 2),
			); err != nil {
				return nil, err
			}
			canvas.AddSpacer(4)

		case kindBody:
			bodyBuf.WriteString(sec.text + "\n")

		case kindFooter:
			footerBuf.WriteString(sec.text + "\n")
		}
	}

	if err := flushBody(); err != nil {
		return nil, err
	}

	// Footer: usage tips / stats – rendered in a muted steel-blue tone
	if footerBuf.Len() > 0 {
		footerText := cleanForImage(strings.TrimRight(footerBuf.String(), "\n"))
		if err := canvas.AddText(footerText,
			textimage.WithFontSize(13),
			textimage.WithFontColor(color.RGBA{R: 148, G: 168, B: 202, A: 255}),
			textimage.WithPadding(28, 12),
		); err != nil {
			return nil, err
		}
	}

	// ── Watermark bar ─────────────────────────────────────────────────────
	elapsed := time.Since(genAt)

	canvas.AddSpacer(10)

	// Thin rule above the watermark
	if err := canvas.AddText(dividerLine,
		textimage.WithFontSize(9),
		textimage.WithAlign(textimage.AlignCenter),
		textimage.WithFontColor(color.RGBA{R: 38, G: 58, B: 105, A: 180}),
		textimage.WithPadding(28, 1),
	); err != nil {
		return nil, err
	}

	// Two-column watermark row: brand name (left) | gen time + duration (right)
	wmColor := color.RGBA{R: 88, G: 105, B: 140, A: 200}
	wmOpts := []textimage.Option{
		textimage.WithFontSize(11),
		textimage.WithFontColor(wmColor),
		textimage.WithPadding(28, 6),
		textimage.WithLineHeight(1.3),
	}
	wmRightOpts := append(wmOpts[:len(wmOpts):len(wmOpts)],
		textimage.WithAlign(textimage.AlignRight),
	)

	rightText := fmt.Sprintf("%s  %s",
		genAt.Format("2006-01-02 15:04:05"),
		formatImageDuration(elapsed),
	)
	if err := canvas.AddRow(
		textimage.RowItem{Text: "Powered by Remilia", TextOpts: wmOpts},
		textimage.RowItem{Text: rightText, TextOpts: wmRightOpts},
	); err != nil {
		return nil, err
	}

	canvas.AddSpacer(14)

	return canvas.ResultPNG()
}

// formatImageDuration formats a duration for the watermark line.
// Uses ASCII-only units to avoid tofu boxes in CJK fonts.
func formatImageDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dus", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// ── Text-cleaning helpers ─────────────────────────────────────────────────────

// cleanForImage prepares a string for image rendering:
//  1. Replaces emoji/pictographs used in the help plugin with simple text/symbol equivalents
//  2. Strips remaining code-points that common CJK fonts (e.g. Microsoft YaHei / Noto CJK)
//     cannot render, preventing "tofu" (□) boxes.
func cleanForImage(s string) string {
	for _, kv := range helpEmojiReplacements {
		s = strings.ReplaceAll(s, kv[0], kv[1])
	}
	return filterUnsupportedRunes(s)
}

// helpEmojiReplacements maps the emoji/symbols used in help.go to characters
// that common CJK fonts reliably render.
// Ordering matters: multi-codepoint sequences (e.g. 🏷️) must appear before
// their single-codepoint prefix (🏷).
var helpEmojiReplacements = [][2]string{
	{"📖", "◆"},
	{"📦", "◆"},
	{"🔌", "◈"},
	{"💡", "◆"},
	{"📝", "◆"},
	{"📊", "◆"},
	{"📌", "◆"},
	{"👤", ""},
	{"📂", ""},
	{"🏷️", ""},
	{"🏷", ""},
	{"🏠", ""},
	{"❌", "[x]"},
	{"\uFE0F", ""}, // variation selector-16
	{"\u200D", ""}, // zero-width joiner
}

// filterUnsupportedRunes discards Unicode code-points that typical CJK fonts
// do not have glyphs for, keeping only:
//   - ASCII (U+0000–U+007F)
//   - CJK Unified Ideographs and extensions
//   - CJK/Japanese/Korean symbols and punctuation
//   - General punctuation, arrows
//   - Box drawing (─ ═ …) and geometric shapes (◆ ● ■ …)
//   - Fullwidth forms
//   - Whitespace
func filterUnsupportedRunes(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r <= 0x7F: // ASCII
			return r
		case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
			return r
		case r >= 0x3400 && r <= 0x4DBF: // CJK Extension A
			return r
		case r >= 0x20000 && r <= 0x2A6DF: // CJK Extension B
			return r
		case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility
			return r
		case r >= 0x3000 && r <= 0x303F: // CJK Symbols & Punctuation (includes 【】)
			return r
		case r >= 0x3040 && r <= 0x31FF: // Hiragana / Katakana / Bopomofo
			return r
		case r >= 0xAC00 && r <= 0xD7AF: // Hangul syllables
			return r
		case r >= 0x2000 && r <= 0x206F: // General Punctuation (em-dash, en-dash …)
			return r
		case r >= 0x2190 && r <= 0x21FF: // Arrows
			return r
		case r >= 0x2500 && r <= 0x257F: // Box Drawing (─ ═ …)
			return r
		case r >= 0x2580 && r <= 0x259F: // Block Elements
			return r
		case r >= 0x25A0 && r <= 0x25FF: // Geometric Shapes (◆ ● ■ …)
			return r
		case r >= 0xFF00 && r <= 0xFFEF: // Fullwidth Forms (ａ Ａ ！)
			return r
		case unicode.IsSpace(r):
			return r
		default:
			return -1 // strip — glyph likely absent in CJK fonts
		}
	}, s)
}
