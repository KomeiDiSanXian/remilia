package sauce

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"
)

// ProcessedImage 预处理后的图片。
type ProcessedImage struct {
	Data   []byte // 重新编码后的图片字节（PNG 或 JPEG）
	Mime   string // Data 对应的 MIME 类型
	Width  int    // 图片宽度（像素）
	Height int    // 图片高度（像素）
}

// PreprocessOptions 预处理选项。
type PreprocessOptions struct {
	// UpscaleSmall 小图（任一边 < MinDimension）放大 2 倍。
	// 裁切图往往很小，放大后再送入依赖全局特征的引擎（如 SauceNAO）命中率更高。
	UpscaleSmall bool
	// MinDimension 触发放大所需的最小边长，默认 400。
	MinDimension int
	// MaxDimension 尺寸上限（超出则等比缩小），默认 7500（IQDB 上限）。
	MaxDimension int
	// MaxBytes 输出体积上限，默认 8MB（IQDB 上限）。
	MaxBytes int64
}

const (
	defaultMinDimension = 400
	defaultMaxDimension = 7500
	defaultMaxBytes     = 8 * 1024 * 1024
	upscaleFactor       = 2
)

// preprocessImage 解码、可选放大、限制尺寸/体积后重新编码图片。
//
// 返回的字节供各引擎 multipart 直传使用。
func preprocessImage(data []byte, opts PreprocessOptions) (*ProcessedImage, error) {
	if opts.MinDimension <= 0 {
		opts.MinDimension = defaultMinDimension
	}
	if opts.MaxDimension <= 0 {
		opts.MaxDimension = defaultMaxDimension
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxBytes
	}

	mime := sniffMime(data)
	format := mimeToFormat(mime)

	img, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码图片失败: %w", err)
	}
	if decodedFormat != "" {
		format = decodedFormat
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// 小图放大：裁切图常小于 400px，放大 2 倍提升特征保留率
	if opts.UpscaleSmall && (w < opts.MinDimension || h < opts.MinDimension) {
		nw, nh := w*upscaleFactor, h*upscaleFactor
		if nw <= opts.MaxDimension && nh <= opts.MaxDimension {
			img = scaleImage(img, nw, nh)
			w, h = nw, nh
		}
	}

	// 超尺寸限制：等比缩小到 MaxDimension 内
	if w > opts.MaxDimension || h > opts.MaxDimension {
		ratio := float64(opts.MaxDimension) / float64(max(w, h))
		nw := int(float64(w) * ratio)
		nh := int(float64(h) * ratio)
		if nw < 1 || nh < 1 {
			return nil, fmt.Errorf("图片尺寸无效: %dx%d", w, h)
		}
		img = scaleImage(img, nw, nh)
		w, h = nw, nh
	}

	out, outFormat, err := encodeWithinLimit(img, format, opts.MaxBytes)
	if err != nil {
		return nil, err
	}

	return &ProcessedImage{
		Data:   out,
		Mime:   formatToMime(outFormat),
		Width:  w,
		Height: h,
	}, nil
}

// scaleImage 将图片缩放为指定尺寸（双线性插值）。
func scaleImage(img image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

// encodeWithinLimit 编码图片并保证体积不超过 limit；必要时降级为低质量 JPEG。
// 返回最终使用的格式名（png 或 jpeg），以便调用方正确报告 MIME。
func encodeWithinLimit(img image.Image, format string, limit int64) ([]byte, string, error) {
	// 仅保留 PNG 原格式；其余（JPEG/GIF/WebP）统一转 JPEG
	if format != "png" {
		format = "jpeg"
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

	// 优先保持原格式（PNG 无损）
	if format == "png" {
		if out, err := encode(0); err == nil && int64(len(out)) <= limit {
			return out, "png", nil
		}
		format = "jpeg"
	}

	for _, q := range []int{85, 75, 60, 45, 30} {
		out, err := encode(q)
		if err != nil {
			return nil, "", fmt.Errorf("编码图片失败: %w", err)
		}
		if int64(len(out)) <= limit {
			return out, format, nil
		}
	}
	return nil, "", fmt.Errorf("图片压缩后仍超过大小限制")
}

// sniffMime 通过内容嗅探图片 MIME 类型。
func sniffMime(data []byte) string {
	if len(data) == 0 {
		return "image/jpeg"
	}
	mime := http.DetectContentType(data)
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	return "image/jpeg"
}

// mimeToFormat 将 MIME 类型映射为 image.Decode 的格式名。
func mimeToFormat(mime string) string {
	switch mime {
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

// formatToMime 将格式名映射回 MIME 类型。
func formatToMime(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
