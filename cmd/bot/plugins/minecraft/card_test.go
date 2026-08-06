package minecraft

import (
	"image/color"
	"testing"
	"time"
)

func TestRenderMCCardOnline(t *testing.T) {
	status := &MCServerStatus{
		Online:   true,
		Host:     "mc.hypixel.net",
		Port:     25565,
		Latency:  42 * time.Millisecond,
		Edition:  "java",
		Version:  "1.21.1",
		Protocol: 767,
		MOTD: []MotdSegment{
			{Text: "Hypixel ", Color: color.RGBA{R: 255, G: 255, B: 85, A: 255}},
			{Text: "Network", Color: color.RGBA{R: 85, G: 255, B: 255, A: 255}},
		},
		MOTDPlain: "Hypixel Network",
		Players: struct {
			Online int
			Max    int
			List   []PlayerInfo
		}{
			Online: 12,
			Max:    100,
			List: []PlayerInfo{
				{Name: "Steve", UUID: "1"},
				{Name: "Alex", UUID: "2"},
				{Name: "Notch", UUID: "3"},
				{Name: "Herobrine", UUID: "4"},
			},
		},
	}
	png, err := renderMCCard(status)
	if err != nil {
		t.Fatalf("renderMCCard: %v", err)
	}
	if len(png) == 0 {
		t.Fatal("渲染结果为空")
	}
	t.Logf("online card: %d bytes", len(png))
}

func TestRenderMCCardOffline(t *testing.T) {
	status := &MCServerStatus{
		Online: false,
		Host:   "dead.server.com",
		Port:   25565,
	}
	png, err := renderMCCard(status)
	if err != nil {
		t.Fatalf("renderMCOfflineCard: %v", err)
	}
	if len(png) == 0 {
		t.Fatal("渲染结果为空")
	}
	t.Logf("offline card: %d bytes", len(png))
}

func TestRenderMCCardBedrock(t *testing.T) {
	status := &MCServerStatus{
		Online:  true,
		Host:    "play.example.com",
		Port:    19132,
		Latency: -1,
		Edition: "bedrock",
		Version: "1.21.0",
		MOTD:    []MotdSegment{{Text: "Bedrock Server", Color: color.White}},
		Players: struct {
			Online int
			Max    int
			List   []PlayerInfo
		}{
			Online: 5,
			Max:    20,
		},
	}
	png, err := renderMCCard(status)
	if err != nil {
		t.Fatalf("renderMCCard bedrock: %v", err)
	}
	if len(png) == 0 {
		t.Fatal("渲染结果为空")
	}
	t.Logf("bedrock card: %d bytes", len(png))
}

func TestRenderMotdImage(t *testing.T) {
	segments := []MotdSegment{
		{Text: "A ", Color: color.RGBA{R: 85, G: 255, B: 85, A: 255}},
		{Text: "Minecraft", Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}},
		{Text: " Server", Color: color.RGBA{R: 85, G: 85, B: 255, A: 255}},
	}
	img, err := renderMotdImage(segments, 400, 16)
	if err != nil {
		t.Fatalf("renderMotdImage: %v", err)
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Fatal("MOTD 图片尺寸无效")
	}
	t.Logf("motd img: %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
}
