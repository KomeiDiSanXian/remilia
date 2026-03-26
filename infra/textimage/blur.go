package textimage

import "image"

// blurRGBA 对 *image.RGBA 就地应用盒式模糊（水平+垂直两趟）。
// passes 次迭代可近似高斯模糊效果（通常 3 次已足够）。
// radius 为模糊半径（像素），0 或负数时直接返回。
func blurRGBA(img *image.RGBA, radius, passes int) {
	if radius < 1 || passes < 1 {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return
	}

	for range passes {
		// ── 水平趟：对每行建前缀和，O(1) 查窗口均值 ────────────────────────
		prefix := make([][4]uint32, w+1)
		for y := range h {
			base := (b.Min.Y+y)*img.Stride + b.Min.X*4
			prefix[0] = [4]uint32{}
			for x := range w {
				off := base + x*4
				prefix[x+1][0] = prefix[x][0] + uint32(img.Pix[off])
				prefix[x+1][1] = prefix[x][1] + uint32(img.Pix[off+1])
				prefix[x+1][2] = prefix[x][2] + uint32(img.Pix[off+2])
				prefix[x+1][3] = prefix[x][3] + uint32(img.Pix[off+3])
			}
			for x := range w {
				lo, hi := blurClamp(x-radius, 0, w), blurClamp(x+radius+1, 0, w)
				cnt := uint32(hi - lo)
				off := base + x*4
				img.Pix[off] = uint8((prefix[hi][0] - prefix[lo][0]) / cnt)
				img.Pix[off+1] = uint8((prefix[hi][1] - prefix[lo][1]) / cnt)
				img.Pix[off+2] = uint8((prefix[hi][2] - prefix[lo][2]) / cnt)
				img.Pix[off+3] = uint8((prefix[hi][3] - prefix[lo][3]) / cnt)
			}
		}

		// ── 垂直趟：对每列建前缀和 ──────────────────────────────────────────
		vprefix := make([][4]uint32, h+1)
		colBuf := make([][4]uint8, h)
		for x := range w {
			vprefix[0] = [4]uint32{}
			for y := range h {
				off := (b.Min.Y+y)*img.Stride + (b.Min.X+x)*4
				vprefix[y+1][0] = vprefix[y][0] + uint32(img.Pix[off])
				vprefix[y+1][1] = vprefix[y][1] + uint32(img.Pix[off+1])
				vprefix[y+1][2] = vprefix[y][2] + uint32(img.Pix[off+2])
				vprefix[y+1][3] = vprefix[y][3] + uint32(img.Pix[off+3])
			}
			for y := range h {
				lo, hi := blurClamp(y-radius, 0, h), blurClamp(y+radius+1, 0, h)
				cnt := uint32(hi - lo)
				colBuf[y][0] = uint8((vprefix[hi][0] - vprefix[lo][0]) / cnt)
				colBuf[y][1] = uint8((vprefix[hi][1] - vprefix[lo][1]) / cnt)
				colBuf[y][2] = uint8((vprefix[hi][2] - vprefix[lo][2]) / cnt)
				colBuf[y][3] = uint8((vprefix[hi][3] - vprefix[lo][3]) / cnt)
			}
			for y := range h {
				off := (b.Min.Y+y)*img.Stride + (b.Min.X+x)*4
				img.Pix[off] = colBuf[y][0]
				img.Pix[off+1] = colBuf[y][1]
				img.Pix[off+2] = colBuf[y][2]
				img.Pix[off+3] = colBuf[y][3]
			}
		}
	}
}

// blurClamp 将 v 限制在 [lo, hi] 区间内。
func blurClamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
