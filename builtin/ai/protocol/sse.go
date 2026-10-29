// sse.go — SSE 行解析：两个 provider 共用的流式读取骨架。
//
// OpenAI 兼容端点与 Anthropic 都用 Server-Sent Events 传输：逐行读取、只关心
// `data:` 负载、空行跳过。差异全部在"负载怎么解释"，因此这里只负责把 resp.Body
// 变成一条条 data 字符串，解释逻辑留在各自的 ChatStream 里。
package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// sseScanner 逐行读取 SSE 流并产出 `data:` 负载。
type sseScanner struct {
	scanner *bufio.Scanner
}

// newSSEScanner 构造 SSE 读取器。
//
// 缓冲上限 256 KiB：单条 SSE 负载可能包含整段长文本或大块 base64 图片，
// bufio.Scanner 默认的 64 KiB 上限会直接报 "token too long" 并中断整条流。
func newSSEScanner(r io.Reader) *sseScanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	return &sseScanner{scanner: sc}
}

// next 前进到下一条 data 负载；流结束时返回 ("", false)。
//
// 兼容两种写法：规范要求 `data: `（带空格），但实际端点会出现 `data:{...}`
// 与带前导空白的行；此外 Anthropic 的 `event: xxx` 行与 SSE 注释行一律跳过。
func (s *sseScanner) next() (string, bool) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			rest, noSpaceOK := strings.CutPrefix(line, "data:")
			if !noSpaceOK {
				continue
			}
			data = rest
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}
		return data, true
	}
	return "", false
}

// err 返回读取过程中遇到的 I/O 错误（流正常结束时为 nil）。
func (s *sseScanner) err() error {
	if err := s.scanner.Err(); err != nil {
		return fmt.Errorf("ai: stream read: %w", err)
	}
	return nil
}
