package songdle

import (
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// newCmdCtx 构造一个群消息事件上下文（平台 qq），内容为完整命令文本。
func newCmdCtx(content, chatID, userID string) *eventctx.Context {
	evt := platform.NewSyntheticEvent(
		platform.EventKindGroupMessage,
		content,
		platform.WithSyntheticPlatform("qq"),
		platform.WithSyntheticChat(platform.ChatInfo{ID: chatID, IsGroup: true}),
		platform.WithSyntheticSender(platform.UserInfo{ID: userID, DisplayName: userID}),
	)
	return eventctx.NewContextFromEvent(evt, &platform.NoopSender{})
}

// testPlugin 构造一个不依赖 i18n/storage 的插件实例（t 回退为 key 本身）。
func testPlugin(t *testing.T) *Plugin {
	t.Helper()
	return &Plugin{
		pool:     newTestPool(t),
		sessions: NewSessionStore(30 * time.Minute),
		cfg: config{
			DefaultTries: defaultTries,
			MaxTries:     defaultMaxTries,
			DefaultScope: ScopeGroup,
		},
		location: time.UTC,
	}
}

// probeTarget 是命令测试用的固定谜底。
var commandTarget = Track{
	ID: "3", Title: "Future", Artist: "★STAR GUiTAR [cover]", Genre: "流行&动漫",
	Type: "SD", Version: "maimai", BPM: 130, MasDS: 10.7, MasBreak: 9,
	Aliases: []string{"未来", "ftr"},
}

// putGame 直接放入一局群维度对局，便于用固定谜底做确定性测试。
func putGame(t *testing.T, p *Plugin, chatID, userID string, target Track, max int) *Game {
	t.Helper()
	now := timeNow()
	g := &Game{
		ID:           "test",
		Platform:     "qq",
		ChatID:       chatID,
		OwnerID:      userID,
		OwnerName:    userID,
		Scope:        ScopeGroup,
		Mode:         ModeRandom,
		Target:       target,
		MaxAttempts:  max,
		Participants: make(map[string]string, 2),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	p.sessions.Put(SessionKey("qq", chatID, "", ScopeGroup), g)
	return g
}

func groupGame(t *testing.T, p *Plugin, chatID string) *Game {
	t.Helper()
	g, ok := p.sessions.Get(SessionKey("qq", chatID, "", ScopeGroup))
	if !ok {
		t.Fatal("应存在群维度对局")
	}
	return g
}

func TestCommandStartCreatesGame(t *testing.T) {
	p := testPlugin(t)
	if err := p.handleSongdle(newCmdCtx("/songdle", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if g.Target.Title == "" {
		t.Fatal("开局应选定谜底")
	}
	if g.OwnerID != "u1" || g.Scope != ScopeGroup {
		t.Errorf("对局元信息错误: owner=%q scope=%v", g.OwnerID, g.Scope)
	}
	if g.MaxAttempts != defaultTries {
		t.Errorf("默认次数 = %d, 期望 %d", g.MaxAttempts, defaultTries)
	}
	if len(g.Probes) != 0 || g.Finished {
		t.Error("开局不应有探测记录或结束")
	}
}

func TestCommandStartRejectsBadTries(t *testing.T) {
	p := testPlugin(t)
	if err := p.handleSongdle(newCmdCtx("/songdle --tries 999", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if p.sessions.Len() != 0 {
		t.Fatal("非法次数不应创建对局")
	}
}

func TestCommandStartTypeFilter(t *testing.T) {
	p := testPlugin(t)
	if err := p.handleSongdle(newCmdCtx("/songdle --type DX", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if g.Target.Type != "DX" {
		t.Fatalf("--type DX 应只出 DX 曲目，实际 %q", g.Target.Type)
	}
}

func TestCommandStartRevealArtist(t *testing.T) {
	p := testPlugin(t)
	if err := p.handleSongdle(newCmdCtx("/songdle --artist", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); !g.RevealArtist {
		t.Error("--artist 应标记公布曲师")
	}
}

func TestCommandProbeShorthand(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	if err := p.handleSongdle(newCmdCtx("/songdle 曲师 ★STAR GUiTAR [cover]", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if g.Attempts() != 1 {
		t.Fatalf("探测应消耗 1 次，实际 %d", g.Attempts())
	}
	if g.Probes[0].Attr != AttrArtist || g.Probes[0].Mark != Match {
		t.Fatalf("曲师应判定命中，实际 %+v", g.Probes[0])
	}
	if g.Finished {
		t.Error("元数据命中不应结束对局")
	}
}

func TestCommandProbeSubcommand(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	if err := p.handleSongdle(newCmdCtx("/songdle probe bpm 100", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if g.Attempts() != 1 || g.Probes[0].Attr != AttrBPM {
		t.Fatalf("probe 子命令应记录 BPM 探测，实际 %+v", g.Probes)
	}
	if g.Probes[0].Dir != DirUp {
		t.Errorf("BPM 100 应提示谜底更高，实际 %v", g.Probes[0].Dir)
	}
}

func TestCommandProbeRepeat(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	for range 2 {
		if err := p.handleSongdle(newCmdCtx("/songdle bpm 130", "chat1", "u1")); err != nil {
			t.Fatalf("handleSongdle: %v", err)
		}
	}
	if g := groupGame(t, p, "chat1"); g.Attempts() != 1 {
		t.Fatalf("重复探测不应增加次数，实际 %d", g.Attempts())
	}
}

func TestCommandProbeBadValue(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	if err := p.handleSongdle(newCmdCtx("/songdle bpm abc", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); g.Attempts() != 0 {
		t.Fatalf("非法取值不应消耗次数，实际 %d", g.Attempts())
	}
}

func TestCommandTitleGuessWins(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	if err := p.handleSongdle(newCmdCtx("/songdle 歌名 future", "chat1", "u2")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if !g.Finished || !g.Won {
		t.Fatalf("猜中曲名后应获胜: finished=%v won=%v", g.Finished, g.Won)
	}
	if g.Attempts() != 1 {
		t.Errorf("猜测次数 = %d, 期望 1", g.Attempts())
	}
	if g.Participants["u2"] == "" {
		t.Error("提交猜测者应计入参与者")
	}
}

func TestCommandTitleAliasWins(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	if err := p.handleSongdle(newCmdCtx("/songdle 未来", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); !g.Finished || !g.Won {
		t.Fatalf("用俗称猜中应获胜: finished=%v won=%v", g.Finished, g.Won)
	}
}

// TestCommandTitleAttributeAlias 验证 name / song 这类属性别名会被当作曲名直猜。
func TestCommandTitleAttributeAlias(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	if err := p.handleSongdle(newCmdCtx("/songdle name Future", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); !g.Finished || !g.Won {
		t.Fatalf("name 别名应作为曲名直猜并获胜: finished=%v won=%v", g.Finished, g.Won)
	}
}

func TestCommandPartialTitleSuggests(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)

	if err := p.handleSongdle(newCmdCtx("/songdle 歌名 fut", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if g.Finished {
		t.Fatal("模糊匹配只是建议，不应结束对局")
	}
	if g.Attempts() != 0 {
		t.Errorf("模糊匹配不应消耗次数，实际 %d", g.Attempts())
	}
}

func TestCommandTitleGuessLoses(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 1)
	if err := p.handleSongdle(newCmdCtx("/songdle 猜 True Love Song", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); !g.Finished || g.Won {
		t.Fatalf("用尽次数应判负: finished=%v won=%v", g.Finished, g.Won)
	}
}

func TestCommandGiveupOnlyOwner(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)
	g := groupGame(t, p, "chat1")

	if err := p.handleSongdle(newCmdCtx("/songdle giveup", "chat1", "u2")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g.Finished {
		t.Fatal("非发起者不应能放弃群内共享对局")
	}

	if err := p.handleSongdle(newCmdCtx("/songdle giveup", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if !g.Finished || g.Won {
		t.Fatalf("发起者放弃后应判负: finished=%v won=%v", g.Finished, g.Won)
	}
}

func TestCommandNotStarted(t *testing.T) {
	p := testPlugin(t)
	if err := p.handleSongdle(newCmdCtx("/songdle 曲师 Kai", "chat1", "u1")); err != nil {
		t.Fatalf("未开局时探测不应报错: %v", err)
	}
	if err := p.handleSongdle(newCmdCtx("/songdle board", "chat1", "u1")); err != nil {
		t.Fatalf("未开局时查看提示板不应报错: %v", err)
	}
	if p.sessions.Len() != 0 {
		t.Fatal("未开局时不应创建对局")
	}
}

func TestCommandDailyDeterministic(t *testing.T) {
	p1 := testPlugin(t)
	p1.cfg.DefaultDaily = true
	if err := p1.handleSongdle(newCmdCtx("/songdle --daily", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g1 := groupGame(t, p1, "chat1")

	p2 := testPlugin(t)
	p2.cfg.DefaultDaily = true
	if err := p2.handleSongdle(newCmdCtx("/songdle --daily", "chat2", "u2")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g2 := groupGame(t, p2, "chat2")

	if g1.Target.ID != g2.Target.ID {
		t.Fatalf("每日题应稳定: %s != %s", g1.Target.Title, g2.Target.Title)
	}
	if !g1.Daily || g1.Mode != ModeDaily {
		t.Error("每日题应标记 Daily/ModeDaily")
	}
}

func TestCommandPool(t *testing.T) {
	p := testPlugin(t)
	if err := p.handleSongdle(newCmdCtx("/songdle pool", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if p.sessions.Len() != 0 {
		t.Fatal("pool 不应创建对局")
	}
}

func TestCommandGuessByIDWins(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 10)
	if err := p.handleSongdle(newCmdCtx("/songdle 猜 3", "chat1", "u2")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if !g.Finished || !g.Won {
		t.Fatalf("用 ID 猜中应获胜: finished=%v won=%v", g.Finished, g.Won)
	}
	if len(g.Guesses) != 1 || g.Guesses[0].Track.ID != "3" || !g.Guesses[0].Solved() {
		t.Fatalf("应记录一次猜中，实际 %+v", g.Guesses)
	}
	if c, ok := g.Guesses[0].Cell(AttrTitle); !ok || c.Mark != Match {
		t.Errorf("曲名列应判定命中，实际 %+v ok=%v", c, ok)
	}
	if g.Participants["u2"] == "" {
		t.Error("提交猜测者应计入参与者")
	}
}

func TestCommandGuessMissConsumesAttempt(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 5)
	if err := p.handleSongdle(newCmdCtx("/songdle 猜 True Love Song", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if g.Finished {
		t.Fatal("还有剩余次数时不应结束")
	}
	if len(g.Guesses) != 1 || g.Attempts() != 1 {
		t.Fatalf("猜错应记录并消耗一次，实际 guesses=%d attempts=%d", len(g.Guesses), g.Attempts())
	}
	if c, _ := g.Guesses[0].Cell(AttrTitle); c.Mark != Miss {
		t.Errorf("非谜底曲名列应为 Miss，实际 %v", c.Mark)
	}
}

func TestCommandGuessRepeatDoesNotConsume(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 5)
	for range 2 {
		if err := p.handleSongdle(newCmdCtx("/songdle 猜 True Love Song", "chat1", "u1")); err != nil {
			t.Fatalf("handleSongdle: %v", err)
		}
	}
	if g := groupGame(t, p, "chat1"); g.Attempts() != 1 {
		t.Fatalf("重复猜测不应增加次数，实际 %d", g.Attempts())
	}
}

func TestCommandGuessNotFoundDoesNotConsume(t *testing.T) {
	p := testPlugin(t)
	putGame(t, p, "chat1", "u1", commandTarget, 5)
	if err := p.handleSongdle(newCmdCtx("/songdle 猜 zzzzz", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); g.Attempts() != 0 {
		t.Fatalf("查无此曲不应消耗次数，实际 %d", g.Attempts())
	}
}

// TestCommandGuessAmbiguousAlias 验证同一俗称对应多首不同曲目时只给候选、不消耗次数。
func TestCommandGuessAmbiguousAlias(t *testing.T) {
	const songs = `{
  "1": {"id":"1","title":"Alpha","artist":"A","type":"SD","version":"maimai","bpm":100,"masds":10,"masbreak":1},
  "2": {"id":"2","title":"Beta","artist":"B","type":"SD","version":"maimai","bpm":120,"masds":11,"masbreak":2},
  "3": {"id":"3","title":"Future","artist":"B","type":"SD","version":"maimai","bpm":130,"masds":10.7,"masbreak":9}
}`
	const aliases = `[
  {"SongID":1,"Alias":["shared"]},
  {"SongID":2,"Alias":["shared"]}
]`
	pool, err := buildPool([]byte(songs), []byte(aliases))
	if err != nil {
		t.Fatalf("buildPool: %v", err)
	}
	p := &Plugin{pool: pool, sessions: NewSessionStore(30 * time.Minute), location: time.UTC}
	putGame(t, p, "chat1", "u1", commandTarget, 5)
	if err := p.handleSongdle(newCmdCtx("/songdle 猜 shared", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); g.Attempts() != 0 || len(g.Guesses) != 0 {
		t.Fatalf("歧义俗称不应消耗次数，实际 attempts=%d guesses=%d", g.Attempts(), len(g.Guesses))
	}
}

// TestResolveGuess 覆盖「猜的是哪一首」的判定：谜底优先、单候选、同名多谱面、真歧义。
func TestResolveGuess(t *testing.T) {
	twinSD := Track{ID: "1", Title: "Twin", Type: "SD"}
	twinDX := Track{ID: "10001", Title: "Twin", Type: "DX"}
	other := Track{ID: "2", Title: "Other"}
	target := Track{ID: "3", Title: "Future"}

	if got, amb := resolveGuess([]Track{twinSD, target, twinDX}, target, Filter{}); got.ID != target.ID || amb != nil {
		t.Errorf("谜底在候选中应直接返回谜底，实际 %+v amb=%v", got, amb)
	}
	if got, amb := resolveGuess([]Track{other}, target, Filter{}); got.ID != "2" || amb != nil {
		t.Errorf("单候选应直接采用，实际 %+v amb=%v", got, amb)
	}
	if got, amb := resolveGuess([]Track{twinSD, twinDX}, target, Filter{}); got.ID != "10001" || amb != nil {
		t.Errorf("同名多谱面应优先 DX，实际 %+v amb=%v", got, amb)
	}
	if got, amb := resolveGuess([]Track{twinSD, twinDX}, target, Filter{Type: "SD"}); got.ID != "1" || amb != nil {
		t.Errorf("应优先匹配本局筛选类型，实际 %+v amb=%v", got, amb)
	}
	if got, amb := resolveGuess([]Track{twinSD, other}, target, Filter{}); got.ID != "" || len(amb) != 2 {
		t.Errorf("不同曲目应返回歧义候选，实际 %+v amb=%v", got, amb)
	}
}

// TestCommandGuessNumericCollision 验证数字同时命中 ID 与俗称时先提示、再用 #ID 精确消歧。
func TestCommandGuessNumericCollision(t *testing.T) {
	const songs = `{
  "9": {"id":"9","title":"Nine","artist":"A","type":"SD","version":"maimai","bpm":100,"masds":10,"masbreak":1},
  "302": {"id":"302","title":"Other","artist":"B","type":"SD","version":"maimai","bpm":120,"masds":11,"masbreak":2},
  "7": {"id":"7","title":"Seven","artist":"C","type":"SD","version":"maimai","bpm":140,"masds":12,"masbreak":3}
}`
	const aliases = `[{"SongID":302,"Alias":["9"]}]`
	pool, err := buildPool([]byte(songs), []byte(aliases))
	if err != nil {
		t.Fatalf("buildPool: %v", err)
	}
	p := &Plugin{pool: pool, sessions: NewSessionStore(30 * time.Minute), location: time.UTC}
	putGame(t, p, "chat1", "u1", Track{ID: "7", Title: "Seven"}, 5)

	// 谜底（7）不在候选里 → 歧义提示，不消耗次数。
	if err := p.handleSongdle(newCmdCtx("/songdle 猜 9", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	if g := groupGame(t, p, "chat1"); g.Attempts() != 0 || len(g.Guesses) != 0 {
		t.Fatalf("歧义数字不应消耗次数，实际 attempts=%d guesses=%d", g.Attempts(), len(g.Guesses))
	}

	// 用 #ID 精确指定别名对应的曲目 302 → 消耗一次并猜错（谜底是 7）。
	if err := p.handleSongdle(newCmdCtx("/songdle 猜 #302", "chat1", "u1")); err != nil {
		t.Fatalf("handleSongdle: %v", err)
	}
	g := groupGame(t, p, "chat1")
	if len(g.Guesses) != 1 || g.Guesses[0].Track.ID != "302" {
		t.Fatalf("显式 ID 应精确指定 302，实际 %+v", g.Guesses)
	}
}
