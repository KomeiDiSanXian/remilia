package fortune

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"
)

// TestEmbeddedAssetsPresent 确认全部内置素材存在且可解码。
func TestEmbeddedAssetsPresent(t *testing.T) {
	for n := 1; n <= 100; n++ {
		for v := range 2 {
			path := omikujiAssetPath(n, v)
			if decodeAsset(path) == nil {
				t.Errorf("签纸素材无法解码: %s", path)
			}
		}
	}

	if len(tarotDeck) != 78 {
		t.Fatalf("牌库应有 78 张牌，实际 %d", len(tarotDeck))
	}
	for short := range tarotDeck {
		path := tarotAssetPath(short)
		if decodeAsset(path) == nil {
			t.Errorf("塔罗牌面无法解码: %s", path)
		}
	}
}

// TestRenderCards 端到端渲染，确认内置素材能拼出尺寸合理的卡片。
func TestRenderCards(t *testing.T) {
	t.Run("omikuji_two_pages", func(t *testing.T) {
		page0 := decodeAsset(omikujiAssetPath(42, 0))
		page1 := decodeAsset(omikujiAssetPath(42, 1))
		data, err := renderOmikujiCard(page0, page1)
		if err != nil {
			t.Fatalf("御神签渲染失败: %v", err)
		}
		assertCardImage(t, data, page0.Bounds().Dx()+page1.Bounds().Dx()+omikujiPageGap)
	})

	t.Run("omikuji_single_page", func(t *testing.T) {
		// 只有一页素材时应正常输出，不报错。
		page0 := decodeAsset(omikujiAssetPath(70, 0))
		data, err := renderOmikujiCard(page0, nil)
		if err != nil {
			t.Fatalf("单页渲染失败: %v", err)
		}
		assertCardImage(t, data, page0.Bounds().Dx())
	})

	t.Run("omikuji_no_pages", func(t *testing.T) {
		// 两页都缺失时必须报错，由调用方降级为错误提示。
		if _, err := renderOmikujiCard(nil, nil); err == nil {
			t.Error("签纸扫描全部缺失时应返回错误")
		}
	})

	t.Run("tarot_reverse", func(t *testing.T) {
		reading := TarotReading{Card: *tarotDeck["ar10"], IsReverse: true}
		data, err := renderTarotSpread([]tarotColumn{{
			Reading: reading,
			Face:    decodeAsset(tarotAssetPath(reading.Card.NameShort)),
		}})
		if err != nil {
			t.Fatalf("塔罗渲染失败: %v", err)
		}
		assertCardImage(t, data, tarotCardWidth)
	})

	t.Run("tarot_without_face", func(t *testing.T) {
		reading := TarotReading{Card: *tarotDeck["wa01"]}
		data, err := renderTarotSpread([]tarotColumn{{Reading: reading}})
		if err != nil {
			t.Fatalf("无牌面时渲染失败: %v", err)
		}
		assertCardImage(t, data, tarotCardWidth)
	})
}

// TestRotate180 确认逆位牌面确实被翻转（用合成图精确校验像素）。
func TestRotate180(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 3))
	tl := color.RGBA{R: 255, A: 255}
	tr := color.RGBA{G: 255, A: 255}
	bl := color.RGBA{B: 255, A: 255}
	br := color.RGBA{R: 255, G: 255, A: 255}
	src.Set(0, 0, tl)
	src.Set(1, 0, tr)
	src.Set(0, 2, bl)
	src.Set(1, 2, br)

	dst := rotate180(src)
	if dst.Bounds() != src.Bounds() {
		t.Fatalf("旋转后尺寸变化: %v -> %v", src.Bounds(), dst.Bounds())
	}
	for _, tc := range []struct {
		name string
		got  color.RGBA
		want color.RGBA
	}{
		{"右下 = 原左上", dst.At(1, 2).(color.RGBA), tl},
		{"左下 = 原右上", dst.At(0, 2).(color.RGBA), tr},
		{"右上 = 原左下", dst.At(1, 0).(color.RGBA), bl},
		{"左上 = 原右下", dst.At(0, 0).(color.RGBA), br},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestRotate180PreservesBounds 确认对真实牌面旋转后尺寸不变。
func TestRotate180PreservesBounds(t *testing.T) {
	src := decodeAsset(tarotAssetPath("ar00"))
	if src == nil {
		t.Fatal("ar00 素材不可用")
	}
	dst := rotate180(src)
	if dst.Bounds().Dx() != src.Bounds().Dx() || dst.Bounds().Dy() != src.Bounds().Dy() {
		t.Fatalf("旋转后尺寸变化: %v -> %v", src.Bounds(), dst.Bounds())
	}
}

// TestJoinHorizontalLayout 确认拼接是逐点保真的：两列各占其位、互不覆盖，
// 间隔处填底色。
//
// 这里直接测拼接本身而非编码后的成品：卡片最终以有损 JPEG 下发，
// 对编码产物做逐点比对会把编解码误差和排版错误混为一谈。
func TestJoinHorizontalLayout(t *testing.T) {
	page0 := decodeAsset(omikujiAssetPath(42, 0))
	page1 := decodeAsset(omikujiAssetPath(42, 1))
	if page0 == nil || page1 == nil {
		t.Fatal("签纸素材不可用")
	}

	img, err := joinHorizontal(paperBg, omikujiPageGap, page0, page1)
	if err != nil {
		t.Fatal(err)
	}

	w0 := page0.Bounds().Dx()
	x1 := w0 + omikujiPageGap
	wantWidth := w0 + page1.Bounds().Dx() + omikujiPageGap
	if img.Bounds().Dx() != wantWidth {
		t.Fatalf("合成图宽度 = %d, 期望 %d", img.Bounds().Dx(), wantWidth)
	}

	// 左侧区域应逐点对应签文页，右侧区域对应解签页。
	// 允许少量通道误差（8-bit 量纲）：签纸素材是有损 WebP（YCbCr），
	// 解码后绘制到 RGBA 画布的取整路径与 At() 不完全一致，实测偏移 1/255。
	const tolerance = 4
	maxDelta := uint32(0)
	check := func(label string, got, want color.Color) {
		d := colorDelta(got, want)
		maxDelta = max(maxDelta, d)
		if d > tolerance {
			t.Errorf("%s 差异过大: %d/255", label, d)
		}
	}

	for _, y := range []int{0, 5, 40, 200} {
		for _, x := range []int{0, 10, w0 / 2, w0 - 1} {
			check(fmt.Sprintf("左侧(%d,%d)", x, y),
				img.At(x, y), page0.At(page0.Bounds().Min.X+x, page0.Bounds().Min.Y+y))
			check(fmt.Sprintf("右侧(%d,%d)", x, y),
				img.At(x1+x, y), page1.At(page1.Bounds().Min.X+x, page1.Bounds().Min.Y+y))
		}
	}
	t.Logf("最大通道误差 = %d/255 (阈值 %d)", maxDelta, tolerance)

	// 两页之间应是签纸底色，而非被画面覆盖。间隔由画布底色直接绘成，
	// 不经过 WebP 解码，因此要求精确相等。
	if d := colorDelta(img.At(w0+omikujiPageGap/2, 0), paperBg); d != 0 {
		t.Errorf("两页间隔处应为底色，差异 %d", d)
	}
}

// TestJoinHorizontalNil 确认 nil 图片被跳过、全部为空时报错。
func TestJoinHorizontalNil(t *testing.T) {
	page0 := decodeAsset(omikujiAssetPath(42, 0))
	if page0 == nil {
		t.Fatal("签纸素材不可用")
	}

	img, err := joinHorizontal(paperBg, omikujiPageGap, page0, nil)
	if err != nil {
		t.Fatalf("nil 图片应被跳过: %v", err)
	}
	if img.Bounds().Dx() != page0.Bounds().Dx() {
		t.Errorf("宽度 = %d, 期望 %d", img.Bounds().Dx(), page0.Bounds().Dx())
	}

	if _, err := joinHorizontal(paperBg, omikujiPageGap, nil, nil); err == nil {
		t.Error("全部为 nil 时应返回错误")
	}
}

// colorDelta 返回两个颜色各通道（含 alpha）差值的最大值，单位为 8-bit 色阶。
// 使用 RGBA() 归一化后右移，因此兼容不同的颜色模型。
func colorDelta(a, b color.Color) uint32 {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return (max(absDiff(ar, br), absDiff(ag, bg), absDiff(ab, bb), absDiff(aa, ba)) + 128) >> 8
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// assertCardImage 校验渲染结果是尺寸合理的图片。
func assertCardImage(t *testing.T, data []byte, wantWidth int) {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("输出不是有效图片: %v", err)
	}
	if img.Bounds().Dx() != wantWidth {
		t.Errorf("卡片宽度 = %d, 期望 %d", img.Bounds().Dx(), wantWidth)
	}
	if img.Bounds().Dy() < 100 {
		t.Errorf("卡片高度异常: %d", img.Bounds().Dy())
	}
}
