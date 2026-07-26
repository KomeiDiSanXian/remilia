package terminal

import "strings"

// sanitizeForTerminal 清洗写入真实终端的文本，移除可被用于攻击的转义序列，
// 但保留 SGR（颜色/字体样式）序列。
//
// # 为什么需要
//
// 适配器把消息内容原样写进一个已被置于 raw 模式的 tty。只要有一个中继
// handler 把远端消息镜像到操作员控制台（把 QQ 群消息转发到终端是这个适配器
// 最常见的用法之一），攻击者就能在消息里塞进控制序列：
//
//   - OSC 52：`ESC ] 52 ; c ; <base64> BEL` 直接改写操作员的系统剪贴板。
//     操作员下一次往 shell 里粘贴，执行的就是攻击者选定的命令。
//   - `ESC [ 2J` / `ESC [ H`：清屏并复位光标，抹掉现场痕迹。
//   - OSC 0：伪造终端窗口标题。
//   - `\r`：回到行首覆写已输出内容，例如把 "[Bot Reply] " 前缀盖掉，
//     让注入的文本看起来像是适配器自己打印的。
//
// # 策略
//
// 只放行 CSI SGR（`ESC [ ... m`，即颜色与字体样式），其余一切转义序列
// （CSI 光标控制、OSC、DCS/PM/APC、单字符 ESC 命令）以及除 \n \t 之外的
// C0/C1 控制字符一律丢弃。这样机器人有意发送的彩色输出不受影响，
// 而所有能操纵终端状态的序列都被拦下。
func sanitizeForTerminal(s string) string {
	if !needsSanitize(s) {
		return s // 常见路径：纯文本，零分配
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		c := s[i]

		if c == 0x1B { // ESC
			consumed, keep := scanEscape(s[i:])
			if keep != "" {
				b.WriteString(keep)
			}
			if consumed == 0 {
				// 落单的 ESC（字符串以 ESC 结尾）：丢弃
				break
			}
			i += consumed
			continue
		}

		// C0 控制字符：仅保留换行与制表符
		if c < 0x20 {
			if c == '\n' || c == '\t' {
				b.WriteByte(c)
			}
			i++
			continue
		}
		if c == 0x7F { // DEL
			i++
			continue
		}

		b.WriteByte(c)
		i++
	}

	return b.String()
}

// needsSanitize 快速判断字符串是否含有需要处理的控制字符。
func needsSanitize(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 && c != '\n' && c != '\t' {
			return true
		}
		if c == 0x7F {
			return true
		}
	}
	return false
}

// scanEscape 解析从 ESC 开始的转义序列。
//
// 返回消耗的字节数，以及需要保留的内容（仅 SGR 序列会被保留，其余返回 ""）。
// 序列不完整时返回 (0, "")，调用方据此终止扫描。
func scanEscape(s string) (consumed int, keep string) {
	if len(s) < 2 {
		return 0, ""
	}

	switch s[1] {
	case '[': // CSI：ESC [ 参数字节 中间字节 最终字节(0x40-0x7E)
		for i := 2; i < len(s); i++ {
			c := s[i]
			if c >= 0x40 && c <= 0x7E { // 最终字节
				if c == 'm' {
					return i + 1, s[:i+1] // SGR：保留
				}
				return i + 1, "" // 其余 CSI：丢弃
			}
			// 参数字节 0x30-0x3F / 中间字节 0x20-0x2F 之外的内容视为序列损坏
			if c < 0x20 || c > 0x3F {
				return i + 1, ""
			}
		}
		return 0, "" // 未终止

	case ']', 'P', '^', '_': // OSC / DCS / PM / APC：以 BEL 或 ST(ESC \) 终止
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 { // BEL
				return i + 1, ""
			}
			if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '\\' { // ST
				return i + 2, ""
			}
		}
		return 0, "" // 未终止：整段丢弃

	default:
		// 双字符 ESC 命令（ESC c、ESC 7 等）
		return 2, ""
	}
}
