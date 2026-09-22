// score.go — 动作相关度打分：关键词信号 + 会话热用加成 + 可选语义余弦。
//
// 打分是"动作选择"这一领域的知识（工具名/描述/类别如何拼成检索文本、各信号
// 权重多少），因此不随检索骨架下沉；分词、重叠度、语义权重与确定性排序见
// builtin/ai/retrieval。
package decision

import (
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/retrieval"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// 打分权重。
const (
	// scoreNameHit 工具名完整出现在查询中。
	scoreNameHit = 4.0
	// scoreNameTok 工具名 token 与查询重叠。
	scoreNameTok = 3.0
	// scoreDescTok 描述/分类 token 与查询重叠。
	scoreDescTok = 1.0
	// scoreUsedBonus 会话内已调用过的工具加成（保持工具连续性）。
	scoreUsedBonus = 1.5
)

// EstimateToolTokens 粗略估算动作 schema 占用的 token 数。
// 用于工具选择时的 token 预算控制。
func EstimateToolTokens(a toolkit.Action) int {
	spec := a.Spec
	n := 12 + len(spec.Name)
	n += len(spec.Description) / 4
	if spec.Parameters.Type != "" {
		n += 16
	}
	for k, p := range spec.Parameters.Properties {
		n += 10 + len(k)
		n += len(p.Description) / 4
		if len(p.Enum) > 0 {
			n += len(p.Enum) * 3
		}
	}
	return n
}

// ToolEmbeddingText 构建单个工具用于嵌入的文本。
// 这是工具选择域的知识（工具的类别与描述如何拼成检索文本），不随检索骨架下沉。
func ToolEmbeddingText(a toolkit.Action) string {
	desc := a.Spec.Description
	if desc == "" {
		desc = "执行命令 " + a.Spec.Name
	}
	cats := strings.Join(a.Spec.Categories, " ")
	return fmt.Sprintf("tool %s categories: %s. %s", a.Spec.Name, cats, desc)
}

// ScoreToolParts 计算动作得分的三个组成部分：
// kw 关键词分（名称命中 + 名称/描述 token + 会话热用）、
// cos embedding 余弦相似度（向量为 nil 时为 0）、
// final 最终分（kw + cos×retrieval.ScoreEmbedW）。
// 排序与可观测日志共用，避免重复计算。
func ScoreToolParts(query string, queryTokens map[string]float64, a toolkit.Action, used bool, queryVec, toolVec []float32) (kw, cos, final float64) {
	q := strings.ToLower(query)
	spec := a.Spec

	if name := strings.ToLower(spec.Name); name != "" && strings.Contains(q, name) {
		kw += scoreNameHit
	}
	kw += scoreNameTok * retrieval.TokenOverlap(queryTokens, retrieval.TokenizeText(spec.Name))
	kw += scoreDescTok * retrieval.TokenOverlap(queryTokens, retrieval.TokenizeText(spec.Description+" "+strings.Join(spec.Categories, " ")))

	if used {
		kw += scoreUsedBonus
	}
	cos = retrieval.SemanticCosine(queryVec, toolVec)
	final = retrieval.RetrievalScore(kw, cos)
	return kw, cos, final
}
