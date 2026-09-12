package fortune

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	// 解码内置资源使用的图片格式：WebP（塔罗牌面与签纸扫描）与 JPEG/PNG（兜底）。
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// embeddedAssets 内置占卜素材。
//
//   - assets/omikuji/<nnn>_<v>.webp — 浅草寺御神签签纸扫描（100 番 × 2 变体）
//   - assets/tarot/<short>.webp     — 公有领域 RWS 塔罗牌面（78 张）
//
// 素材来源与授权见 assets/ATTRIBUTION.md。
//
//go:embed assets/omikuji/*.webp
//go:embed assets/tarot/*.webp
var embeddedAssets embed.FS

// omikujiAssetPath 返回指定番号与变体的签纸资源路径。
func omikujiAssetPath(number, variant int) string {
	return fmt.Sprintf("assets/omikuji/%03d_%d.webp", number, variant)
}

// tarotAssetPath 返回指定塔罗牌的牌面资源路径。
func tarotAssetPath(nameShort string) string {
	return "assets/tarot/" + nameShort + ".webp"
}

// decodeAsset 从内置资源中解码图片。资源缺失或损坏时返回 nil。
func decodeAsset(path string) image.Image {
	data, err := embeddedAssets.ReadFile(path)
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

// omikujiPageCount 每番签纸的扫描页数：签文页 + 解签页。
const omikujiPageCount = 2

// omikujiPages 返回指定番号签纸的全部扫描页。
//
// 第 0 页为签文页（番号、吉凶与漢詩），第 1 页为解签页（逐句解释与分类运势）。
// 素材缺失的页面会被跳过，返回的切片可能为空或仅一页。
func (p *Plugin) omikujiPages(number int) []image.Image {
	pages := make([]image.Image, 0, omikujiPageCount)
	for variant := range omikujiPageCount {
		path := omikujiAssetPath(number, variant)
		img := decodeAsset(path)
		if img == nil {
			p.errf("fortune: 签纸素材不可用: %s", path)
			continue
		}
		pages = append(pages, img)
	}
	return pages
}

// tarotImage 返回塔罗牌面图，失败时返回 nil（调用方只渲染文字）。
func (p *Plugin) tarotImage(nameShort string) image.Image {
	path := tarotAssetPath(nameShort)
	img := decodeAsset(path)
	if img == nil {
		p.errf("fortune: 塔罗牌面素材不可用: %s", path)
	}
	return img
}

// errf 在 logger 尚未注入时安全地记录错误。
func (p *Plugin) errf(format string, args ...any) {
	if p.log == nil {
		return
	}
	p.log.Errorf(format, args...)
}
