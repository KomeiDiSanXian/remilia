// Package imagekit 提供发送前图片压缩能力，供各插件（sauce / pic 等）复用。
//
// 典型场景：图片体积或边长超过平台限制（如 QQ 图片软限制 20MB，超过会降级
// 为文件类型），或超过发送层分片上传阈值时，先压缩再发送。
package imagekit

import (
	"bytes"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// DefaultMaxBytes 发送前图片体积上限（字节）。
//
// 与 QQ 发送层分片上传阈值（5MB）一致：压到该值以下可同时规避平台软限制
// 降级与更长的分片上传链路。
const DefaultMaxBytes int64 = 5 * 1024 * 1024

// DefaultMaxDimension 发送前图片最长边上限（像素）。
const DefaultMaxDimension = 4096

// DefaultMaxDecodeBytes 解码像素缓冲的默认预算（字节）。
//
// 图片解码为 RGBA 后约占 width×height×4 字节，一张 8000×8000 的图就是
// 256MB——这是压缩路径上最大的瞬时内存开销。预估解码体积超过预算的图
// 跳过重编码（原样返回），把峰值内存约束在可控范围内。
// 128MB ≈ 5760×5760，已覆盖 MaxDimension=4096 所需的最大工作区。
const DefaultMaxDecodeBytes int64 = 128 * 1024 * 1024

// Options 控制图片压缩行为。
type Options struct {
	// MaxBytes 输出体积上限（字节），<=0 表示不限制体积。
	MaxBytes int64
	// MaxDimension 输出最长边上限（像素），<=0 表示不限制边长。
	MaxDimension int
	// MaxDecodeBytes 解码像素缓冲预算（字节）：预估解码体积
	// （width×height×4）超过该值时跳过重编码，原样返回。
	// 0 = 使用 DefaultMaxDecodeBytes；负值 = 不限制（不推荐，
	// 超大尺寸图会产生数百 MB 的瞬时内存峰值）。
	MaxDecodeBytes int64
}

// Result 压缩结果。
type Result struct {
	// Data 输出的图片字节；未重编码时为原数据。
	Data []byte
	// Mime 输出的 MIME 类型。
	Mime string
	// Reencoded 是否发生了重编码（false = 原样返回）。
	Reencoded bool
}

// Compress 将图片压缩到 Options 限定的体积与边长内。
//
// 规则：
//   - 体积与边长均未超限 → 原样返回（Reencoded=false）
//   - GIF 动图跳过（避免动图被压成静态帧）
//   - 超限时等比缩放并重编码：PNG 优先无损保留，超限转 JPEG 逐档降质量
//     （85 → 75 → 60 → 45 → 30）
//   - 解码/编码失败 → 原样返回，不阻塞发送
func Compress(data []byte, mime string, opts Options) Result {
	if len(data) == 0 {
		return Result{Data: data, Mime: mime}
	}
	if opts.MaxBytes <= 0 && opts.MaxDimension <= 0 {
		return Result{Data: data, Mime: mime}
	}
	if int64(len(data)) <= opts.MaxBytes && !exceedsDimension(data, opts.MaxDimension) {
		return Result{Data: data, Mime: mime}
	}
	if isGIF(data, mime) {
		return Result{Data: data, Mime: mime}
	}
	if exceedsDecodeBudget(data, decodeBudget(opts.MaxDecodeBytes)) {
		return Result{Data: data, Mime: mime}
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{Data: data, Mime: mime}
	}

	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if opts.MaxDimension > 0 && (w > opts.MaxDimension || h > opts.MaxDimension) {
		ratio := float64(opts.MaxDimension) / float64(max(w, h))
		nw, nh := int(float64(w)*ratio), int(float64(h)*ratio)
		if nw < 1 || nh < 1 {
			return Result{Data: data, Mime: mime}
		}
		img = scale(img, nw, nh)
	}

	out, outFormat, err := encodeWithinLimit(img, formatOf(mime), opts.MaxBytes)
	if err != nil {
		return Result{Data: data, Mime: mime}
	}
	return Result{Data: out, Mime: formatToMime(outFormat), Reencoded: true}
}

// exceedsDimension 报告图片任一边是否超过 maxDimension。
// 仅解码图片配置（不解码像素），失败时视为未超限。
func exceedsDimension(data []byte, maxDimension int) bool {
	if maxDimension <= 0 {
		return false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return false
	}
	return cfg.Width > maxDimension || cfg.Height > maxDimension
}

// isGIF 报告图片是否为 GIF（按 MIME 或文件头判断），动图不应被压成静态帧。
func isGIF(data []byte, mime string) bool {
	if strings.EqualFold(mime, "image/gif") {
		return true
	}
	return bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))
}

// decodeBudget 解析 Options.MaxDecodeBytes：0 = 默认预算，负值 = 不限制（返回 0）。
func decodeBudget(v int64) int64 {
	switch {
	case v < 0:
		return 0
	case v == 0:
		return DefaultMaxDecodeBytes
	default:
		return v
	}
}

// exceedsDecodeBudget 报告图片解码后的像素缓冲预估体积（width×height×4）
// 是否超过预算。仅读取图片头（DecodeConfig），不解码像素，本身开销可忽略。
// 头部解析失败时视为未超限（交给后续解码失败路径兜底）。
func exceedsDecodeBudget(data []byte, budget int64) bool {
	if budget <= 0 {
		return false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return false
	}
	return int64(cfg.Width)*int64(cfg.Height)*4 > budget
}

// scale 将图片缩放为指定尺寸（双线性插值）。
func scale(img image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

// formatOf 将 MIME 映射为内部格式名。
func formatOf(mime string) string {
	switch strings.ToLower(mime) {
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "jpeg"
	}
}

// formatToMime 将内部格式名映射回 MIME。
func formatToMime(format string) string {
	if format == "png" {
		return "image/png"
	}
	return "image/jpeg"
}

// encodeWithinLimit 编码图片并保证体积不超过 limit（<=0 表示不限制）。
// 仅保留 PNG 原格式（无损）；其余统一转 JPEG，必要时逐档降质量。
func encodeWithinLimit(img image.Image, format string, limit int64) ([]byte, string, error) {
	if format != "png" {
		format = "jpeg"
	}
	if limit <= 0 {
		limit = 200 * 1024 * 1024 // 仅限尺寸时给一个宽松的体积上限
	}

	encode := func(q int) ([]byte, error) {
		var buf bytes.Buffer
		if format == "png" {
			if err := png.Encode(&buf, img); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		}
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}

	if format == "png" {
		if out, err := encode(0); err == nil && int64(len(out)) <= limit {
			return out, "png", nil
		}
		format = "jpeg"
	}

	for _, q := range []int{85, 75, 60, 45, 30} {
		out, err := encode(q)
		if err != nil {
			return nil, "", err
		}
		if int64(len(out)) <= limit {
			return out, format, nil
		}
	}
	return nil, "", errLimit
}

// errLimit 表示图片压缩后仍超过体积上限。
var errLimit = errLimitError{}

type errLimitError struct{}

func (errLimitError) Error() string { return "image still exceeds size limit after compression" }
