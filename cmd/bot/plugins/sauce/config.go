package sauce

import (
	"time"
)

// ── 配置读取 ───────────────────────────────────────────────────────────
//
// 所有配置项均从 plugins.sauce 命名空间读取，支持热重载；缺省时使用
// 与 config.example.yaml 一致的默认值。

// proxy 返回引擎请求代理地址（如 "http://127.0.0.1:7890"）。
// 空值沿用环境变量代理或直连；Setup 时读取一次（修改需重启）。
func (p *Plugin) proxy() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("proxy", "")
}

// apiKey 返回 SauceNAO API Key，未配置时返回空字符串。
func (p *Plugin) apiKey() string {
	if p.cfg == nil {
		return ""
	}
	return p.cfg.GetString("saucenao_api_key", "")
}

// maxResults 返回最多展示的结果数（默认 3，非法值回退为 3）。
func (p *Plugin) maxResults() int {
	n := 3
	if p.cfg != nil {
		n = p.cfg.GetInt("max_results", 3)
	}
	if n <= 0 {
		return 3
	}
	return n
}

// saucenaoDB 返回 SauceNAO 检索数据库 ID（999 = 全部）。
func (p *Plugin) saucenaoDB() int {
	if p.cfg == nil {
		return 999
	}
	return p.cfg.GetInt("saucenao_db", 999)
}

// sendThumbnails 是否逐条附带缩略图图片发送结果。
func (p *Plugin) sendThumbnails() bool {
	return p.cfg != nil && p.cfg.GetBool("send_thumbnails", false)
}

// enableIQDB 是否启用 IQDB 引擎（默认开启）。
func (p *Plugin) enableIQDB() bool {
	return p.cfg == nil || p.cfg.GetBool("enable_iqdb", true)
}

// enableTraceMoe 是否启用 trace.moe 引擎（默认开启）。
func (p *Plugin) enableTraceMoe() bool {
	return p.cfg == nil || p.cfg.GetBool("enable_trace_moe", true)
}

// enableAnimeTrace 是否启用 AnimeTrace 引擎（默认开启）。
func (p *Plugin) enableAnimeTrace() bool {
	return p.cfg == nil || p.cfg.GetBool("enable_animetrace", true)
}

// similarityThreshold 全局相似度阈值。存在不低于阈值的匹配时只展示这些，
// 否则保留全部（避免裁切/滤镜图被全部过滤）。0 = 不过滤。
func (p *Plugin) similarityThreshold() float64 {
	if p.cfg == nil {
		return 60
	}
	return p.cfg.GetFloat64("similarity_threshold", 60)
}

// traceMoeMinSimilarity trace.moe 最低相似度（0-100），过滤无意义弱匹配。
func (p *Plugin) traceMoeMinSimilarity() float64 {
	if p.cfg == nil {
		return 75
	}
	return p.cfg.GetFloat64("trace_moe_min_similarity", 75)
}

// upscaleSmall 是否在上传前将小图（任一边 < 400px）放大 2 倍。
func (p *Plugin) upscaleSmall() bool {
	return p.cfg == nil || p.cfg.GetBool("upscale_small", true)
}

// reportErrors 是否在结果中上报引擎失败原因。
func (p *Plugin) reportErrors() bool {
	return p.cfg == nil || p.cfg.GetBool("report_errors", true)
}

// imageWaitTimeout 等待用户发送图片的超时时间（默认 60s）。
func (p *Plugin) imageWaitTimeout() time.Duration {
	if p.cfg == nil {
		return 60 * time.Second
	}
	return p.cfg.GetDuration("image_wait_timeout", 60*time.Second)
}

// searchTimeout 单次检索的总体超时（下载+预处理+全部引擎并发）。
// 默认 90s：IQDB 高峰期排队时单次请求可达 60s，重试 1 次后仍须在预算内完成。
func (p *Plugin) searchTimeout() time.Duration {
	if p.cfg == nil {
		return 90 * time.Second
	}
	return p.cfg.GetDuration("search_timeout", 90*time.Second)
}

// iqdbTimeout IQDB 单次请求超时（默认 45s，排队高峰期单次可达 60s）。
func (p *Plugin) iqdbTimeout() time.Duration {
	if p.cfg == nil {
		return 45 * time.Second
	}
	return p.cfg.GetDuration("iqdb_timeout", 45*time.Second)
}

// iqdbRetries IQDB 失败重试次数（默认 1，0 = 不重试）。
func (p *Plugin) iqdbRetries() int {
	n := 1
	if p.cfg != nil {
		n = p.cfg.GetInt("iqdb_retries", 1)
	}
	if n < 0 {
		return 0
	}
	return n
}

// iqdbGrace 其他引擎结果就绪后，为 IQDB 额外等待的宽限期（默认 10s）。
//
// 宽限期内 IQDB 返回 → 与其他引擎结果合并为一条消息发送；
// 超时 → 先发送其他引擎结果并提示"IQDB 排队中"，IQDB 完成后补发。
func (p *Plugin) iqdbGrace() time.Duration {
	if p.cfg == nil {
		return 10 * time.Second
	}
	return p.cfg.GetDuration("iqdb_grace", 10*time.Second)
}
