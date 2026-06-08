// Package css 提供中国空间站(天宫)实时轨道追踪插件。
//
// 从中国载人航天工程办公室(CMSE)官网下载官方 CCSDS OEM 轨道数据，
// 计算当前位置、高度、速度，并通过折线图展示近期轨道高度变化趋势。
package css

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
	"github.com/KomeiDiSanXian/remilia/cmd/bot/plugins/satutil"
)

// Plugin 中国空间站追踪插件实例。
type Plugin struct {
	mu      sync.RWMutex
	oem     *OEMEphemeris
	probes  []*health.APIProbe
	dataDir string
	log     plugin.Logger
}

// Option CSS 插件配置函数。
type Option func(*Plugin)

// WithDataDir 设置缓存目录。路径为空时不缓存 OEM 数据（每次启动重新下载）。
func WithDataDir(path string) Option {
	return func(p *Plugin) { p.dataDir = path }
}

// New 创建中国空间站追踪插件的 Descriptor。
//
// 命令: /css
// 回复: 带位置、高度趋势折线图的图片卡片
// AI:   get_css_location() 工具 + css_query 技能
// 数据源: 中国载人航天工程办公室 (CMSE) 官方 OEM 数据
func New(opts ...Option) *plugin.Descriptor {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}
	return &plugin.Descriptor{
		Name:    "css",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "中国空间站(天宫)实时轨道位置、高度变化趋势与轨道分析",
			Category:    "工具",
			Tags:        []string{"CSS", "中国空间站", "天宫", "天和"},
			HelpText: `中国空间站 — 基于中国载人航天工程办公室官方 OEM 数据

用法：
  /css`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			cmseProbe := health.NewAPIProbe("cmse.gov.cn", "https://www.cmse.gov.cn", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			p.probes = []*health.APIProbe{cmseProbe}

			// 创建缓存目录
			if p.dataDir != "" {
				if err := os.MkdirAll(p.dataDir, 0755); err != nil {
					ctx.Log.Warnf("Failed to create css cache dir: %v", err)
				}
			}

			// 尝试从缓存加载（确保启动后即使网络不可达也能快速响应）
			cached := LoadCache(p.dataDir)
			if cached != nil && cached.Covers(time.Now()) {
				p.mu.Lock()
				p.oem = cached
				p.mu.Unlock()
				ctx.Log.Infof("Loaded CSS OEM from cache: %s ~ %s",
					cached.StartTime.Format("01-02 15:04"), cached.StopTime.Format("01-02 15:04"))
			}

			// 后台尝试下载最新 OEM 数据
			if err := p.refreshOEM(context.Background()); err != nil {
				ctx.Log.Warnf("Initial OEM download failed: %v", err)
				if p.oem == nil {
					ctx.Log.Warn("No cached OEM data available, waiting for background refresh")
				}
			}

			// 启动后台健康探测
			for _, pr := range p.probes {
				ctx.Spawn(func(runCtx context.Context) {
					pr.StartBackground(runCtx, 1*time.Minute)
				})
			}

			// 后台每 60 分钟自动刷新 OEM 数据
			ctx.Spawn(func(runCtx context.Context) {
				p.backgroundRefresh(runCtx)
			})

			ctx.OnCommand("", "/css", p.handleCSS)

			if aiSvc, ok := plugin.TryService[*ai.Plugin](ctx, "ai"); ok {
				aiSvc.RegisterToolProvider(p)
				aiSvc.RegisterSkillProvider(p)
			}

			return p, nil
		},
		Teardown: func(ctx *plugin.TeardownContext) error {
			p.mu.RLock()
			oem := p.oem
			p.mu.RUnlock()
			if oem != nil && p.dataDir != "" {
				if err := oem.SaveCache(p.dataDir); err != nil {
					logger.Warnf("[css] Failed to cache OEM data on teardown: %v", err)
				}
			}
			return nil
		},
	}
}

// backgroundRefresh 后台定时刷新 OEM 轨道数据。
func (p *Plugin) backgroundRefresh(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := p.refreshOEM(ctx); err != nil {
				logger.Warnf("[css] OEM refresh failed: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// refreshOEM 从 CMSE 官网下载最新的 OEM 轨道数据并更新缓存。
// 先下载再比较，确保失败时不影响现有数据，下一轮 ticker 会自动重试。
func (p *Plugin) refreshOEM(ctx context.Context) error {
	dlURL, err := getLatestDownloadURL(ctx)
	if err != nil {
		return fmt.Errorf("get download URL: %w", err)
	}

	oem, err := downloadAndParseWithCache(ctx, dlURL, p.dataDir)
	if err != nil {
		return fmt.Errorf("download and parse OEM: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.oem != nil && p.oem.SourceURL == dlURL && p.oem.CreationDate.Equal(oem.CreationDate) {
		logger.Debug("[css] OEM data is up-to-date, skip refresh")
		return nil
	}

	p.oem = oem
	logger.Infof("[css] OEM data refreshed: %s", dlURL)
	return nil
}

// handleCSS 处理 /css 命令。
//
// 从缓存的 OEM 数据中计算当前位置、高度、速度，
// 并渲染含高度趋势折线图的图片卡片。
func (p *Plugin) handleCSS(ctx *eventctx.Context) error {
	p.mu.RLock()
	oem := p.oem
	p.mu.RUnlock()

	if oem == nil {
		return ctx.ReplyError("CSS 轨道数据尚未就绪，请稍后再试")
	}
	if !oem.Covers(time.Now()) {
		return ctx.ReplyError("CSS 轨道数据已过期，请稍后再试")
	}

	now := time.Now()
	lat, lng, alt, ok := computePosition(oem, now)
	if !ok {
		return ctx.ReplyError("无法计算当前 CSS 轨道位置")
	}

	vel := computeSpeed(oem, now)
	history := computeAltHistory(oem, 4*time.Hour)
	trend := computeTrend(history)

	png, err := renderCard(lat, lng, alt, vel, history, trend, oem)
	if err != nil {
		return ctx.ReplyText(formatCSSText(lat, lng, alt, vel, trend, oem))
	}

	_, err = ctx.Reply(platform.ImageDataMessage(png, "css.png", "image/png"))
	return err
}

// ListTools 返回可供 AI 调用的工具列表。
// 实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "get_css_location",
			Description: "获取中国空间站(天宫)的实时轨道位置、高度、速度及高度变化趋势",
			Parameters: ai.ToolParamSchema{
				Type:       "object",
				Properties: map[string]ai.ToolParamSchema{},
			},
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				p.mu.RLock()
				oem := p.oem
				p.mu.RUnlock()

				if oem == nil {
					return "", fmt.Errorf("CSS data not available")
				}
				if !oem.Covers(time.Now()) {
					return "", fmt.Errorf("CSS data expired")
				}

				now := time.Now()
				lat, lng, alt, ok := computePosition(oem, now)
				if !ok {
					return "", fmt.Errorf("cannot compute CSS position")
				}
				return formatCSSText(lat, lng, alt, computeSpeed(oem, now),
					computeTrend(computeAltHistory(oem, 4*time.Hour)), oem), nil
			},
		},
	}
}

// ListSkills 返回可供 AI 使用的技能列表。
// 实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "css_query",
			Description: "查询中国空间站(天宫)实时轨道信息",
			Prompt:      "你是一个空间站信息助手。当用户询问中国空间站时，使用 get_css_location 工具获取实时数据，然后以自然语言总结位置、高度、速度、轨道高度变化趋势等信息。",
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

// formatCSSText 将 CSS 数据格式化为纯文本（备用方案）。
func formatCSSText(lat, lng, alt, vel float64, trend Trend, oem *OEMEphemeris) string {
	period := satutil.OrbitalPeriod(alt)
	minLat, maxLat, minLng, maxLng := satutil.VisibleBounds(lat, lng, alt)

	gmst := satutil.GMST(time.Now())
	ex, ey, ez := satutil.GeodeticToECEF(lat, lng, alt)
	ix, iy, iz := satutil.ECEFtoECI(ex, ey, ez, gmst)
	eclipse := satutil.IsInEclipse(ix, iy, iz, gmst)
	eclipseLabel := "[光照]"
	if eclipse {
		eclipseLabel = "[地影]"
	}

	text := fmt.Sprintf("[CSS] 中国空间站 - 天宫 (轨道预报)\n纬度: %s\n经度: %s\n高度: %.1f km\n速度: %.2f km/s\n轨道周期: %.1f min\n可见区域: 纬度 %.0f~%.0f  经度 %.0f~%.0f\n光照: %s\n近地点: %.1f km\n远地点: %.1f km\n",
		fmtLat(lat), fmtLng(lng), alt, vel, period, minLat, maxLat, minLng, maxLng, eclipseLabel, trend.MinAlt, trend.MaxAlt)
	if trend.Slope != 0 {
		dir := "[上升]"
		if trend.Slope < 0 {
			dir = "[下降]"
		}
		abs := trend.Slope
		if abs < 0 {
			abs = -abs
		}
		if abs < 1 {
			text += fmt.Sprintf("轨道趋势: %s %.0f m/天\n", dir, abs*1000)
		} else {
			text += fmt.Sprintf("轨道趋势: %s %.2f km/天\n", dir, abs)
		}
	}
	text += "数据来源: 中国载人航天工程办公室 (cmse.gov.cn)\n基于 CMSE 7 天轨道预报"
	return text
}
