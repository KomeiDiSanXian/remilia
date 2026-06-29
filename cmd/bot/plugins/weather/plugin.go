// Package weather 提供天气查询插件。
//
// 同时请求 wttr.in、Open-Meteo、WeatherAPI 三个天气 API，
// 取最快响应结果渲染为图片卡片返回。不可用的 API 会被健康探针跳过。
package weather

import (
	"context"
	"fmt"
	"time"

	"github.com/KomeiDiSanXian/remilia/command"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"

	"github.com/KomeiDiSanXian/remilia/builtin/ai"
)

// Plugin 天气查询插件实例。
type Plugin struct {
	probes  []*health.APIProbe
	sources []*weatherSource
	log     plugin.Logger
}

// weatherSource 封装一个天气 API 源及其健康探针。
type weatherSource struct {
	name  string
	fetch func(ctx context.Context, city string) (*Result, error)
	probe *health.APIProbe
}

// Result 统一的天气查询结果。
type Result struct {
	City          string
	TempC         float64
	FeelsLikeC    float64
	Humidity      int
	WindSpeedKmph float64
	WindDir       string  // 风向（中文）
	PressureMB    float64 // 气压 (hPa)
	Condition     string
	VisibilityKM  float64
	UV            int
	Cloud         int
	Source        string
}

// New 创建天气查询插件的 Descriptor。
//
// 命令: /weather <城市名>
// 回复: 带天气信息的图片卡片
// AI:   get_weather(city) 工具 + weather_query 技能
func New() *plugin.Descriptor {
	p := &Plugin{}
	return &plugin.Descriptor{
		Name:    "weather",
		Version: "1.0.0",
		Meta: &plugin.Metadata{
			Author:      "Remilia Community",
			Description: "天气查询，同时请求多个天气 API 并以最快响应展示",
			Category:    "工具",
			Tags:        []string{"天气", "查询", "生活"},
			HelpText: `天气查询 — 同时从 wttr.in、Open-Meteo、WeatherAPI 获取天气数据，取最快结果

用法：
  /weather <城市名>

示例：
  /weather 北京
  /weather Tokyo`,
		},
		Setup: func(ctx *plugin.SetupContext) (any, error) {
			p.log = ctx.Log

			wttrProbe := health.NewAPIProbe("wttr.in", "https://wttr.in", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			omProbe := health.NewAPIProbe("open-meteo", "https://api.open-meteo.com", 5*time.Second, health.WithMaxSeverity(health.Degraded))
			waProbe := health.NewAPIProbe("weatherapi", "https://api.weatherapi.com", 5*time.Second, health.WithMaxSeverity(health.Degraded))

			p.probes = []*health.APIProbe{wttrProbe, omProbe, waProbe}

			p.sources = []*weatherSource{
				{name: "wttr.in", fetch: fetchWttr, probe: wttrProbe},
				{name: "Open-Meteo", fetch: fetchOpenMeteo, probe: omProbe},
				{name: "WeatherAPI", fetch: fetchWeatherAPI, probe: waProbe},
			}

			// 启动后台健康探测，每分钟检查各 API 可用性
			for _, pr := range p.probes {
				ctx.Spawn(func(runCtx context.Context) {
					pr.StartBackground(runCtx, 1*time.Minute)
				})
			}

			weatherDef := command.NewDef("weather").Description("天气查询").
				Arg("city", "城市名称", true).
				Example("/weather 北京").Example("/weather Tokyo").Build()
			ctx.OnCommandDefWith("", "/weather", weatherDef, p.handleWeather, eventctx.OnMentionedBotOrNoMentions())

			return p, nil
		},
	}
}

// handleWeather 处理 /weather 命令。
//
// 并发调用各天气源，取最快响应渲染为图片卡片。
// 所有源均失败时返回错误提示。
func (p *Plugin) handleWeather(ctx *eventctx.Context) error {
	parsed, err := eventctx.ParseCommand(ctx)
	if err != nil || len(parsed.Positional) == 0 {
		ctx.ReplyError("请输入城市名，例如：/weather 北京"); return nil
	}
	city := parsed.Positional[0]

	reqCtx, cancel := context.WithTimeout(ctx.Context(), 8*time.Second)
	defer cancel()

	type raceResp struct {
		r      *Result
		source string
	}

	ch := make(chan raceResp, len(p.sources))

	for _, src := range p.sources {
		if !src.probe.IsHealthy() {
			continue
		}
		src := src
		go func() {
			r, err := src.fetch(reqCtx, city)
			if err != nil {
				return
			}
			ch <- raceResp{r, src.name}
		}()
	}

	select {
	case res := <-ch:
		res.r.Source = res.source
		png, err := renderCard(res.r)
		if err != nil {
			ctx.ReplyText(formatWeatherText(res.r)); return nil
		}
		ctx.Reply(platform.ImageDataMessage(png, "weather.png", "image/png"))
		return err
	case <-reqCtx.Done():
		ctx.ReplyError("天气服务暂时不可用，请稍后再试"); return nil
	}
}

// ListTools 返回可供 AI 调用的工具列表。
// 实现 ai.ToolProvider。
func (p *Plugin) ListTools() []ai.Tool {
	return []ai.Tool{
		{
			Name:        "get_weather",
			Categories:  []string{"weather"},
			Description: "获取指定城市的当前天气信息，包含温度、湿度、风速等",
			Parameters: ai.ToolParamSchema{
				Type: "object",
				Properties: map[string]ai.ToolParamSchema{
					"city": {Type: "string", Description: "城市名称，如 北京、Tokyo、London"},
				},
				Required: []string{"city"},
			},
			Execute: func(gctx context.Context, args map[string]any) (string, error) {
				city, _ := args["city"].(string)
				if city == "" {
					return "", fmt.Errorf("city is required")
				}
				reqCtx, cancel := context.WithTimeout(gctx, 8*time.Second)
				defer cancel()

				type raceResp struct {
					r *Result
				}
				ch := make(chan raceResp, len(p.sources))
				for _, src := range p.sources {
					if !src.probe.IsHealthy() {
						continue
					}
					src := src
					go func() {
						r, err := src.fetch(reqCtx, city)
						if err != nil {
							return
						}
						ch <- raceResp{r}
					}()
				}
				select {
				case res := <-ch:
					return formatWeatherText(res.r), nil
				case <-reqCtx.Done():
					return "", fmt.Errorf("all weather APIs failed")
				}
			},
		},
	}
}

// ListSkills 返回可供 AI 使用的技能列表。
// 实现 ai.SkillProvider。
func (p *Plugin) ListSkills() []ai.Skill {
	return []ai.Skill{
		{
			Name:        "weather_query",
			Description: "查询天气信息并进行分析",
			Prompt:      "你是一个天气助手。当用户询问某个城市的天气时，使用 get_weather 工具获取数据，然后以自然语言总结天气情况，包括温度、湿度、风速等，并给出出行建议。",
			Tools:       p.ListTools(),
		},
	}
}

// HealthCheckers 返回插件的所有 API 健康探针，实现 health.CheckProvider。
func (p *Plugin) HealthCheckers() []health.Checker {
	out := make([]health.Checker, len(p.probes))
	for i, pr := range p.probes {
		out[i] = pr
	}
	return out
}

// formatWeatherText 将天气结果格式化为纯文本（备用方案，图片渲染失败时使用）。
func formatWeatherText(r *Result) string {
	windDir := r.WindDir
	if windDir == "" {
		windDir = "-"
	}
	return fmt.Sprintf("[Weather] %s\n温度: %.1f °C\n体感: %.1f °C\n湿度: %d %%\n风速: %.1f km/h\n风向: %s\n气压: %.1f hPa\n天气: %s\n可见度: %.1f km\n紫外线: %d\n云量: %d %%\n来源: %s",
		r.City, r.TempC, r.FeelsLikeC, r.Humidity, r.WindSpeedKmph, windDir, r.PressureMB, r.Condition, r.VisibilityKM, r.UV, r.Cloud, r.Source)
}
