package dealornodeal

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"
	"testing"
)

func decodePNG(t *testing.T, data []byte) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("生成的图片不是合法 PNG: %v", err)
	}
	if img.Bounds().Dx() < 300 || img.Bounds().Dy() < 300 {
		t.Fatalf("图片尺寸过小: %v", img.Bounds())
	}
}

func TestRenderStage_AllPhases(t *testing.T) {
	withDeterministicRNG(t)
	p := &Plugin{}

	pick := NewGame(Options{})
	opening := NewGame(Options{})
	if err := opening.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	offer := NewGame(Options{})
	if err := offer.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := openWholeRound(offer); err != nil {
		t.Fatalf("openWholeRound: %v", err)
	}

	final := reachFinal(t)
	finished := reachFinal(t)
	if err := finished.Keep(); err != nil {
		t.Fatalf("Keep: %v", err)
	}

	for name, g := range map[string]*Game{
		"pick": pick, "opening": opening, "offer": offer,
		"final": final, "finished": finished,
	} {
		view := p.stageViewFor(g)
		data, err := renderStage(view)
		if err != nil {
			t.Fatalf("%s: renderStage: %v", name, err)
		}
		decodePNG(t, data)
		if txt := renderStageText(view); txt == "" {
			t.Fatalf("%s: 文本降级为空", name)
		}
	}
}

func TestRenderStage_Invalid(t *testing.T) {
	if _, err := renderStage(stageView{}); err == nil {
		t.Fatal("空数据应返回错误")
	}
	if _, err := renderStage(stageView{Values: []int64{1, 2}}); err == nil {
		t.Fatal("Opened 与 Values 长度不符应返回错误")
	}
}

// TestRenderStageText_RevealsAllAtFinish 结算阶段应揭晓全部箱子的金额，
// 而不是只显示对局中已经打开的那几个。
func TestRenderStageText_RevealsAllAtFinish(t *testing.T) {
	withDeterministicRNG(t)
	g := NewGame(Options{})
	if err := g.Pick(1); err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if err := openWholeRound(g); err != nil {
		t.Fatalf("openWholeRound: %v", err)
	}
	if err := g.Deal(); err != nil {
		t.Fatalf("Deal: %v", err)
	}

	view := (&Plugin{}).stageViewFor(g)
	if view.Phase != PhaseFinished {
		t.Fatalf("阶段 = %v, 期望 finished", view.Phase)
	}
	txt := renderStageText(view)
	for i, val := range g.Values {
		token := fmt.Sprintf("[%d:%s]", i+1, shortMoney(val, "$"))
		if i == g.OwnCase {
			token = fmt.Sprintf("[%d:YOU %s]", i+1, shortMoney(val, "$"))
		}
		if !strings.Contains(txt, token) {
			t.Errorf("结算文本缺少 %s：\n%s", token, txt)
		}
	}
}

func TestMoneyFormatting(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{123456, "123,456"},
		{1000000, "1,000,000"},
	}
	for _, c := range cases {
		if got := groupDigits(c.in); got != c.want {
			t.Errorf("groupDigits(%d) = %q, 期望 %q", c.in, got, c.want)
		}
	}
	if got := money(123456, "$"); got != "$123,456" {
		t.Errorf("money = %q", got)
	}
	short := map[int64]string{750: "$750", 25000: "$25K", 400000: "$400K", 1000000: "$1M"}
	for in, want := range short {
		if got := shortMoney(in, "$"); got != want {
			t.Errorf("shortMoney(%d) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestPhaseLabelAndBanner(t *testing.T) {
	view := stageView{Phase: PhaseOpening, ToOpenThisRound: 3, Values: DefaultValues}
	if got := phaseLabel(view); got == "" {
		t.Error("phaseLabel 不应为空")
	}
	if got := bannerText(view, "$"); got == "" {
		t.Error("bannerText 不应为空")
	}
}
