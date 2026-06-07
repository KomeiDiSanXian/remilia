// Package iss 提供国际空间站(ISS)实时追踪插件。
//
// 获取 ISS 实时位置、轨道高度、速度及在轨航天员信息，
// 并通过后台轮询累积历史高度数据，绘制面积图展示轨道高度变化趋势。
package iss

import (
	"context"
	"fmt"
	"strings"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/kv"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/satutil"
)

var _ health.CheckProvider = (*Plugin)(nil)

// Plugin ISS 追踪插件实例。
type Plugin struct {
	tracker *Tracker
	probes  []*health.APIProbe
	kvPath  string
	store   *kv.DB
	log     plugin.Logger
}

// Option ISS 插件配置函数。
type Option func(*Plugin)

// WithDataDir 设置持久化数据目录。路径为空时使用纯内存模式（重启后历史丢失）。
func WithDataDir(path string) Option {
	return func(p *Plugin) { p.kvPath = path }
}

// New 创建 ISS 追踪插件的 Descriptor。
//
// 命令: /iss
// 回复: 带位置、高度折线图和航天员信息的图片卡片
// AI:   get_iss_location() 工具 + iss_query 技能
func New(opts ...Option) *plugin.Descriptor {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}
	return &plugin.Descriptor{
		Name:    "iss",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "国际空间站实时位置、轨道高度变化与在轨航天员信息",
			Category:    "工具",
			Tags:        []string{"ISS", "空间站", "航天"},
			HelpText: `国际空间站 — 实时位置、轨道高度趋势与在轨航天员

用法：
  /iss`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log
			p.tracker = NewTracker()

			// 从持久化存储加载历史记录
			if !ctx.DryRun && p.kvPath != "" {
				db, err := kv.Open(p.kvPath)
				if err != nil {
					ctx.Log.Warnf("Failed to open iss kv store: %v, using in-memory only", err)
				} else {
					p.store = db
					if err := p.tracker.Load(db); err != nil {
						ctx.Log.Warnf("Failed to load iss history: %v", err)
					} else {
						ctx.Log.Infof("Loaded %d ISS altitude records from store", p.tracker.Count())
					}
				}
			}

			issProbe := health.NewAPIProbe("wheretheiss.at", "https://api.wheretheiss.at/v1/satellites/25544", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			astroProbe := health.NewAPIProbe("open-notify.org", "http://api.open-notify.org/astros.json", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{issProbe, astroProbe}

			// 启动后台健康探测
			for _, pr := range p.probes {
				ctx.Spawn(func(runCtx context.Context) {
					pr.StartBackground(runCtx, 1*time.Minute)
				})
			}

			// 启动轨道高度历史追踪（每5分钟轮询），有新记录时保存
			ctx.Spawn(func(runCtx context.Context) {
				p.tracker.Start(runCtx, 5*time.Minute, func() {
					if p.store != nil {
						if err := p.tracker.Save(p.store); err != nil {
							p.log.Warnf("Failed to save iss history: %v", err)
						}
					}
				})
			})

			ctx.OnCommand("", "/iss", p.handleIss)

			if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
				aiSvc.RegisterToolProvider(p)
				aiSvc.RegisterSkillProvider(p)
			}

			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			if p.store != nil {
				if err := p.tracker.Save(p.store); err != nil {
					p.log.Warnf("Failed to save iss history on teardown: %v", err)
				}
				p.store.Close()
			}
			return nil
		},
	}
}

// handleIss 处理 /iss 命令。
//
// 获取 ISS 当前位置、在轨航天员和历史高度数据，
// 渲染为图片卡片（含高度面积图）。
func (p *Plugin) handleIss(ctx *eventctx.Context) error {
	reqCtx, cancel := context.WithTimeout(ctx.Context(), 10*time.Second)
	defer cancel()

	type result struct {
		pos    *IssPosition
		astros []string
		count  int
	}

	ch := make(chan *result, 1)
	errCh := make(chan error, 1)

	go func() {
		pos, err := fetchPosition(reqCtx)
		if err != nil {
			errCh <- err
			return
		}
		astros, count, err := fetchAstros(reqCtx)
		if err != nil {
			errCh <- err
			return
		}
		ch <- &result{pos: pos, astros: astros, count: count}
	}()

	select {
	case r := <-ch:
		history := p.tracker.GetRecent(0)
		trend := computeTrend(history)
		png, err := renderCard(r.pos, r.astros, r.count, history, trend)
		if err != nil {
			return ctx.ReplyText(formatISSText(r.pos, r.astros, r.count, trend))
		}
		_, err = ctx.Reply(platform.ImageDataMessage(png, "iss.png", "image/png"))
		return err
	case err := <-errCh:
		return ctx.ReplyError(fmt.Sprintf("ISS 数据获取失败: %v", err))
	case <-reqCtx.Done():
		return ctx.ReplyError("ISS 服务暂时不可用，请稍后再试")
	}
}

// ListTools 返回可供 AI 调用的工具列表。
// 实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "get_iss_location",
			Description: "获取国际空间站(ISS)的实时位置、高度、速度以及当前在轨航天员信息",
			Parameters: ai.ToolParamSchema{
				Type:       "object",
				Properties: map[string]ai.ToolParamSchema{},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				reqCtx, cancel := context.WithTimeout(gctx, 10*time.Second)
				defer cancel()

				pos, err := fetchPosition(reqCtx)
				if err != nil {
					return "", err
				}
				astros, count, err := fetchAstros(reqCtx)
				if err != nil {
					return "", err
				}
				return formatISSText(pos, astros, count, Trend{}), nil
			},
		},
	}
}

// ListSkills 返回可供 AI 使用的技能列表。
// 实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "iss_query",
			Description: "查询国际空间站实时信息",
			Prompt:      "你是一个空间站信息助手。当用户询问国际空间站时，使用 get_iss_location 工具获取实时数据，然后以自然语言总结位置、高度、速度和航天员信息。",
			Tools:       p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的 API 健康探针，实现 health.CheckProvider。
func (p *Plugin) HealthCheckers() []health.Checker {
	out := make([]health.Checker, len(p.probes))
	for i, pr := range p.probes {
		out[i] = pr
	}
	return out
}

// formatISSText 将 ISS 数据格式化为纯文本（备用方案）。
func formatISSText(pos *IssPosition, astros []string, count int, trend Trend) string {
	period := satutil.OrbitalPeriod(pos.Altitude)
	minLat, maxLat, minLng, maxLng := satutil.VisibleBounds(pos.Latitude, pos.Longitude, pos.Altitude)
	vis := "[光照]"
	if pos.Visibility == "eclipsed" {
		vis = "[地影]"
	}
	var text strings.Builder
	text.WriteString(fmt.Sprintf("[ISS] 国际空间站\n纬度: %s\n经度: %s\n高度: %.1f km\n速度: %.2f km/s\n轨道周期: %.1f min\n可见区域: 纬度 %.0f~%.0f  经度 %.0f~%.0f\n光照: %s\n在轨: %d人\n",
		fmtLat(pos.Latitude), fmtLng(pos.Longitude), pos.Altitude, pos.Velocity/3600, period, minLat, maxLat, minLng, maxLng, vis, count))
	if trend.Slope != 0 && count >= 3 {
		dir := "[上升]"
		if trend.Slope < 0 {
			dir = "[下降]"
		}
		abs := trend.Slope
		if abs < 0 {
			abs = -abs
		}
		if abs < 1 {
			text.WriteString(fmt.Sprintf("轨道趋势: %s %.0f m/天\n", dir, abs*1000))
		} else {
			text.WriteString(fmt.Sprintf("轨道趋势: %s %.2f km/天\n", dir, abs))
		}
	}
	for _, name := range astros {
		text.WriteString(fmt.Sprintf("  . %s\n", name))
	}
	return text.String()
}
