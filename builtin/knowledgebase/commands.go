package knowledgebase

import (
	"context"
	"fmt"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// handleKB 处理 /kb 子命令（rebuild / status），均需 superadmin。
func (p *Plugin) handleKB(ctx *eventctx.Context) error {
	parsed := ctx.GetParsedCommand()
	if len(parsed.CommandPath) < 2 {
		p.replyFormatted(ctx, kbCommandHelp())
		return nil
	}
	if !isSuperAdmin(ctx) {
		p.replyFormatted(ctx, "❌ 知识库管理需要 superadmin 角色")
		return nil
	}

	switch parsed.CommandPath[1] {
	case "rebuild":
		return p.handleRebuild(ctx)
	case "status":
		p.replyFormatted(ctx, p.statsText())
		return nil
	default:
		p.replyFormatted(ctx, kbCommandHelp())
		return nil
	}
}

// handleRebuild 异步重建索引：立即回复"开始构建"，后台执行完成后无法推送
// 主动消息，通过 /kb status 或 kb_stats 查看结果。
func (p *Plugin) handleRebuild(ctx *eventctx.Context) error {
	if p.store == nil {
		p.replyFormatted(ctx, "❌ 知识库未启用（enabled=false），无法重建")
		return nil
	}
	if p.building.Load() {
		p.replyFormatted(ctx, "⏳ 知识库正在重建中，请稍后再试")
		return nil
	}
	p.replyFormatted(ctx, "🔄 开始重建知识库索引……（进度可用 /kb status 查看）")
	p.spawn(func() {
		buildCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := p.Build(buildCtx, false); err != nil {
			p.setLastErr(err)
			p.logf("knowledgebase: /kb rebuild failed: %v", err)
		}
	})
	return nil
}

// kbCommandHelp 返回 /kb 命令帮助文本。
func kbCommandHelp() string {
	return fmt.Sprintf(`📋 **知识库管理**

  ` + "`/kb rebuild`" + `   — 重建知识库索引（增量；文档变更后自动构建，一般无需手动）
  ` + "`/kb status`" + `    — 查看知识库索引状态

权限：需 superadmin 角色。
查询请直接让 AI 使用 kb_search 工具。`)
}
