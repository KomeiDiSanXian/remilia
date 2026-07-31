package sauce

// ── 统一结果类型 ──────────────────────────────────────────────────────

// SearchResult 是各搜索引擎返回结果的统一模型。
//
// 不同引擎填充的字段有所差异：
//   - Source / Similarity / Thumbnail / ExtURLs / SourceName 所有引擎都会尽量填充
//   - Episode / Timestamp / PreviewURL / VideoURL 仅 TraceMoe 使用
//   - Hits 由 mergeResults 跨引擎去重时累计（单引擎结果恒为 1）
type SearchResult struct {
	Source     string   // 主命中的搜索引擎："SauceNAO" / "ASCII2D" / "IQDB" / "TraceMoe"；多引擎命中时以 "+" 拼接
	Similarity string   // 相似度百分比字符串（如 "95.40"），无相似度的引擎可为空
	Title      string   // 作品标题（IQDB 为标签串，SauceNAO 为作品名，TraceMoe 为番名）
	Author     string   // 作者名（可为空）
	Thumbnail  string   // 缩略图 URL
	ExtURLs    []string // 外部来源链接
	SourceName string   // 来源站点名（Pixiv / Twitter / Danbooru / Yande.re 等）
	Episode    string   // TraceMoe：命中话数
	Timestamp  string   // TraceMoe：命中时间点（如 "03:12"）
	PreviewURL string   // TraceMoe：命中画面预览图 URL
	VideoURL   string   // TraceMoe：命中片段预览视频 URL
	Hits       int      // 命中该结果的引擎数（跨引擎去重后 ≥1）
}

// engineInput 是各搜索引擎的公共入参。
//
//   - ImageURL：可公开访问的原始图片地址。部分引擎（如 ASCII2D）依赖它
//     服务端自行抓图，因此必须提供。
//   - Data：本地下载并预处理后的图片字节。SauceNAO / IQDB / TraceMoe
//     优先使用 Data 以 multipart 直传，不依赖图片 URL 在引擎侧的可达性。
//   - Mime：Data 对应的 MIME 类型（如 "image/jpeg"），可为空。
type engineInput struct {
	ImageURL string
	Data     []byte
	Mime     string
}
