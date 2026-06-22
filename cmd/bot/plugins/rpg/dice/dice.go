// Package dice 提供通用骰子引擎，作为 RPG 插件体系的基础依赖层。
//
// 支持标准骰子表达式语法：
//   - NdM — N 个 M 面骰子（如 2d20）
//   - NdM+K / NdM-K — 带加值/减值（如 1d20+5）
//   - NdM^K — N 个中取最高 K 个（如 4d6^3，D&D 属性生成）
//   - NdMvK — N 个中取最低 K 个（如 2d8v1，劣势场景）
//   - 多表达式组合：2d20+1d6+3
//
// 本包导出 [Servicer] 接口和 [Service] 实现，供 coc、dnd 等插件的
// Setup 阶段通过 plugin.TryService[dice.Servicer] 获取。
//
// 命令:
//   - /r <表达式> — 通用掷骰
//   - /d <面数> [数量] — 简写掷骰
//   - /rh <表达式> — 暗骰
//   - /roll — 随机 D100
//
// AI 工具:
//   - roll_dice(expression) → 掷骰结果文本
//
// AI 技能:
//   - dice_master — 骰子大师技能，自动识别用户掷骰需求
package dice

import (
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	rng   *rand.Rand
	rngMu sync.Mutex
)

func init() {
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))
}

func rngIntn(n int) int {
	rngMu.Lock()
	defer rngMu.Unlock()
	return rng.Intn(n)
}

// RollResult 一次掷骰的完整结果，包含解析后的详细数据和格式化文本。
type RollResult struct {
	Expr    string       // 原始表达式
	Total   int          // 最终总和
	Details []SingleRoll // 每组骰子的详细信息
	Raw     string       // 格式化结果文本（带 emoji 和 markdown）
}

// SingleRoll 一组同型骰子的详细信息。
type SingleRoll struct {
	Count   int   // 骰子个数
	Sides   int   // 骰子面数
	Results []int // 每次掷骰的结果
	Mod     int   // 加值/减值
	Keep    int   // 取最高 K 个（0 表示不启用）
	KeepLow int   // 取最低 K 个（0 表示不启用）
}

// Servicer 骰子服务接口，供其他插件通过 plugin.TryService 获取。
//
// coc 和 dnd 插件在 Setup 阶段依赖此接口完成所有掷骰操作，
// 便于测试时注入 mock。
type Servicer interface {
	// Roll 解析并执行骰子表达式。
	Roll(expr string) (*RollResult, error)
	// RollD 简化的按面数掷骰。
	RollD(count, sides int) (*RollResult, error)
	// RollWithReason 带原因的掷骰，结果文本会包含原因前缀。
	RollWithReason(expr, reason string) (*RollResult, error)
}

// Service 是 [Servicer] 的标准实现，线程安全。
type Service struct{}

var exprRe = regexp.MustCompile(`(\d*)d(\d+)(?:\^(\d+)|v(\d+))?(?:([+-]\d+))?`)

func (s *Service) Roll(expr string) (*RollResult, error) {
	return roll(expr)
}

func (s *Service) RollD(count, sides int) (*RollResult, error) {
	return roll(fmt.Sprintf("%dd%d", count, sides))
}

func (s *Service) RollWithReason(expr, reason string) (*RollResult, error) {
	r, err := roll(expr)
	if err != nil {
		return nil, err
	}
	r.Raw = fmt.Sprintf("【%s】%s", reason, r.Raw)
	return r, nil
}

// roll 是内部核心函数，解析骰子表达式并执行掷骰。
// 表达式按 "+" 拆分，每个部分独立处理再求和。
func roll(expr string) (*RollResult, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("表达式不能为空")
	}

	input := expr
	total := 0
	var details []SingleRoll

	for _, part := range regexp.MustCompile(`\+`).Split(expr, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		negate := false
		if strings.HasPrefix(part, "-") {
			negate = true
			part = strings.TrimPrefix(part, "-")
		}

		m := exprRe.FindStringSubmatch(part)
		if m == nil {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("无法解析: %q", part)
			}
			if negate {
				n = -n
			}
			total += n
			continue
		}

		count, _ := strconv.Atoi(m[1])
		if count == 0 {
			count = 1
		}
		sides, _ := strconv.Atoi(m[2])
		keep := 0
		keepLow := 0
		if m[3] != "" {
			keep, _ = strconv.Atoi(m[3])
		}
		if m[4] != "" {
			keepLow, _ = strconv.Atoi(m[4])
		}
		mod := 0
		if m[5] != "" {
			mod, _ = strconv.Atoi(m[5])
		}

		if sides < 1 {
			return nil, fmt.Errorf("面数必须大于0")
		}
		if count < 1 || count > 1000 {
			return nil, fmt.Errorf("骰子数量需在 1-1000 之间")
		}
		if keep > count {
			return nil, fmt.Errorf("取高数量不能超过骰子总数")
		}
		if keepLow > count {
			return nil, fmt.Errorf("取低数量不能超过骰子总数")
		}

		results := make([]int, count)
		for i := 0; i < count; i++ {
			results[i] = rngIntn(sides) + 1
		}

		partTotal := 0
		if keep > 0 {
			sorted := make([]int, count)
			copy(sorted, results)
			sort.Sort(sort.Reverse(sort.IntSlice(sorted)))
			partTotal = sum(sorted[:keep])
		} else if keepLow > 0 {
			sorted := make([]int, count)
			copy(sorted, results)
			sort.Ints(sorted)
			partTotal = sum(sorted[:keepLow])
		} else {
			partTotal = sum(results)
		}

		if negate {
			mod = -mod
			partTotal = -partTotal
		}

		partTotal += mod
		total += partTotal

		details = append(details, SingleRoll{
			Count:   count,
			Sides:   sides,
			Results: results,
			Mod:     mod,
			Keep:    keep,
			KeepLow: keepLow,
		})
	}

	raw := formatResult(total, details)
	return &RollResult{Expr: input, Total: total, Details: details, Raw: raw}, nil
}

func formatResult(total int, details []SingleRoll) string {
	var parts []string
	for _, d := range details {
		p := fmt.Sprintf("%dD%d", d.Count, d.Sides)
		var resStrs []string
		for _, r := range d.Results {
			resStrs = append(resStrs, strconv.Itoa(r))
		}
		joined := strings.Join(resStrs, ", ")
		showTotal := sum(d.Results)
		if d.Keep > 0 {
			p += fmt.Sprintf("^%d", d.Keep)
		} else if d.KeepLow > 0 {
			p += fmt.Sprintf("v%d", d.KeepLow)
		}
		rollPart := fmt.Sprintf("[%s] = %d", joined, showTotal)
		if d.Mod != 0 {
			rollPart += fmt.Sprintf(" (%+d)", d.Mod)
			showTotal += d.Mod
		}
		_ = showTotal
		parts = append(parts, fmt.Sprintf("%s %s", p, rollPart))
	}

	var r strings.Builder
	if len(parts) == 1 {
		r.WriteString(parts[0])
		fmt.Fprintf(&r, " = **%d**", total)
	} else {
		r.WriteString(strings.Join(parts, " + "))
		fmt.Fprintf(&r, "\n合计 = **%d**", total)
	}
	return r.String()
}

func sum(vals []int) int {
	s := 0
	for _, v := range vals {
		s += v
	}
	return s
}
