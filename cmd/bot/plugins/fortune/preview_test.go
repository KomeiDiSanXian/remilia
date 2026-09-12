package fortune

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestPreviewRender 把样例卡片写入 FORTUNE_PREVIEW_DIR 指定的目录，用于人工确认渲染效果。
//
// 默认跳过，需要时执行：
//
//	FORTUNE_PREVIEW_DIR=%TEMP%\fortune_preview go test ./cmd/bot/plugins/fortune/ -run TestPreviewRender -v
func TestPreviewRender(t *testing.T) {
	dir := os.Getenv("FORTUNE_PREVIEW_DIR")
	if dir == "" {
		t.Skip("未设置 FORTUNE_PREVIEW_DIR")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(name string, data []byte, err error) {
		if err != nil {
			t.Errorf("%s 渲染失败: %v", name, err)
			return
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Errorf("%s 写入失败: %v", name, err)
			return
		}
		t.Logf("saved %s (%d bytes)", path, len(data))
	}

	// 御神签：两页（签文页 + 解签页）合成的完整签纸。
	for _, n := range []int{1, 42, 70} {
		data, err := renderOmikujiCard(
			decodeAsset(omikujiAssetPath(n, 0)),
			decodeAsset(omikujiAssetPath(n, 1)),
		)
		write(fmt.Sprintf("omikuji_%03d.png", n), data, err)
	}

	// 每种花色各取一张，正逆位成对输出，便于核对逆位是否翻转。
	for _, short := range []string{"ar10", "wa01", "cu07"} {
		for _, reverse := range []bool{false, true} {
			reading := &TarotReading{Card: *tarotDeck[short], IsReverse: reverse}
			data, err := renderTarotCard(reading, decodeAsset(tarotAssetPath(short)))
			write("tarot_"+short+"_"+reading.Orientation()+".png", data, err)
		}
	}
}
