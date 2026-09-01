package qqpanel

import (
	stdctx "context"
	"encoding/json"
	"strings"
	"testing"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/platform"
	qqplatform "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/plugin/plugintest"
	"github.com/KomeiDiSanXian/remilia/testbot"
	"github.com/tidwall/gjson"
)

// stubAPI 包装 testbot.MockAPI，允许覆盖面板/菜单接口的返回结果并记录请求。
type stubAPI struct {
	*testbot.MockAPI

	panelListResult gjson.Result
	panelListErr    error

	createPanelResult gjson.Result
	createPanelErr    error
	createdReq        *dto.CreatePanelRequest

	deletePanelErr error
	deletedPanelID string

	menuResult gjson.Result
	menuErr    error

	menuUpdateErr error
	menuUpdateReq *dto.UpdateMenuRequest
}

func (s *stubAPI) GetPanelList(stdctx.Context, string, string, int) (gjson.Result, error) {
	return s.panelListResult, s.panelListErr
}

func (s *stubAPI) CreatePanel(_ stdctx.Context, req *dto.CreatePanelRequest) (gjson.Result, error) {
	s.createdReq = req
	return s.createPanelResult, s.createPanelErr
}

func (s *stubAPI) DeletePanel(_ stdctx.Context, panelID string) (gjson.Result, error) {
	s.deletedPanelID = panelID
	return gjson.Result{}, s.deletePanelErr
}

func (s *stubAPI) GetMenu(stdctx.Context) (gjson.Result, error) {
	return s.menuResult, s.menuErr
}

func (s *stubAPI) UpdateMenu(_ stdctx.Context, req *dto.UpdateMenuRequest) (gjson.Result, error) {
	s.menuUpdateReq = req
	return gjson.Result{}, s.menuUpdateErr
}

var _ openapi.OpenAPI = (*stubAPI)(nil)

// syncDispatcher 同步执行发送任务，使 Reply 在测试中立即完成。
type syncDispatcher struct{}

func (syncDispatcher) Submit(_ string, task func(stdctx.Context) error) error {
	return task(stdctx.Background())
}

// qqEvent 构造 QQ 平台事件。
func qqEvent(evType dto.EventType, detail map[string]any) platform.Event {
	raw, _ := json.Marshal(detail)
	return qqplatform.NewEvent(&dto.Payload{
		ID:        "evt001",
		Type:      evType,
		Operation: dto.Dispatch,
		Detail:    raw,
	})
}

// groupCtx 构造带 QQ 群消息事件的上下文。
func groupCtx(api openapi.OpenAPI, content string) *eventctx.Context {
	ev := qqEvent(dto.GroupAtMessageCreate, map[string]any{
		"id":           "msg001",
		"content":      content,
		"timestamp":    "2026-09-01T12:00:00+08:00",
		"author":       map[string]any{"member_openid": "openid_u1", "username": "alice"},
		"group_openid": "group001",
		"message_type": 0,
	})
	ctx := eventctx.NewContextFromEvent(ev, qqplatform.NewSender(api))
	ctx.SetDispatcher(syncDispatcher{})
	return ctx
}

// c2cCtx 构造带 QQ C2C 消息事件的上下文。
func c2cCtx(api openapi.OpenAPI, content string) *eventctx.Context {
	ev := qqEvent(dto.C2CMessageCreate, map[string]any{
		"id":        "msg001",
		"content":   content,
		"timestamp": "2026-09-01T12:00:00+08:00",
		"author":    map[string]any{"id": "u1", "user_openid": "openid_u1"},
	})
	ctx := eventctx.NewContextFromEvent(ev, qqplatform.NewSender(api))
	ctx.SetDispatcher(syncDispatcher{})
	return ctx
}

// lastReply 返回最近一次通过 QQ sender 发送的文本。
func lastReply(api *stubAPI) string {
	last := api.LastSent()
	if last == nil {
		return ""
	}
	return last.Content
}

// newTestPlugin 注册插件并返回实例，命令表来自 eng。
func newTestPlugin(t *testing.T, eng *engine.Engine) *Plugin {
	t.Helper()
	d := New()
	info := &plugintest.MockPluginInfo{CoordinatorValue: eng}
	ctx := plugintest.NewSetupContextWithInfo("qqpanel", info, nil)
	defer plugintest.StopSetupContext(ctx)
	svc, err := d.Setup(ctx)
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	p, ok := svc.(*Plugin)
	if !ok {
		t.Fatalf("unexpected service type %T", svc)
	}
	return p
}

func newEngineWithCommands(commands ...string) *engine.Engine {
	eng := engine.NewEngine()
	for _, c := range commands {
		eng.OnCommand("", c)
	}
	return eng
}

// fakeReader 是 engine.Reader 的桩实现，用于精确控制命令列表。
type fakeReader struct {
	cmds []engine.CommandInfo
}

func (f *fakeReader) GetAllCommands() []engine.CommandInfo { return f.cmds }
func (f *fakeReader) FindCommand(name string) *engine.CommandInfo {
	for i := range f.cmds {
		if f.cmds[i].Command == name {
			return &f.cmds[i]
		}
	}
	return nil
}
func (f *fakeReader) GetCommandsByPlugin() map[string][]engine.CommandInfo {
	return map[string][]engine.CommandInfo{"fake": f.cmds}
}
func (f *fakeReader) GetCommandsByCategory() map[string][]engine.CommandInfo {
	return map[string][]engine.CommandInfo{"fake": f.cmds}
}
func (f *fakeReader) GetMatcherCount() int                 { return len(f.cmds) }
func (f *fakeReader) GetMatcherStats() engine.MatcherStats { return engine.MatcherStats{} }
func (f *fakeReader) GetMaxMatchers() int                  { return 0 }
func (f *fakeReader) GetTempMatcherCount() int             { return 0 }

var _ engine.Reader = (*fakeReader)(nil)

func TestDescriptor(t *testing.T) {
	d := New()
	if d == nil {
		t.Fatal("New returned nil")
	}
	if d.Name != "qqpanel" {
		t.Errorf("name = %q, want qqpanel", d.Name)
	}
	if d.Version == "" {
		t.Error("version is empty")
	}
	if d.Meta == nil || d.Meta.Description == "" {
		t.Error("meta description is empty")
	}
	if d.Setup == nil {
		t.Error("Setup is nil")
	}
}

func TestSetup(t *testing.T) {
	p := newTestPlugin(t, newEngineWithCommands("/help"))
	if p == nil {
		t.Fatal("plugin instance is nil")
	}
	if p.info == nil {
		t.Error("info not captured")
	}
}

func TestTruncateWeight(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"", 10, ""},
		{"hello", 3, "hel"},
		{"/help", 14, "/help"},
		{"中文测试", 4, "中文"},  // 每个中文字符权重 2
		{"a中b文", 5, "a中b"}, // 1+2+1=4 ≤5，再加"文"(2) 会超
		{"abcdef", 0, ""},
	}
	for _, c := range cases {
		if got := truncateWeight(c.in, c.max); got != c.want {
			t.Errorf("truncateWeight(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestBuildPanelItems(t *testing.T) {
	eng := newEngineWithCommands("/b", "/a", "/c", "/b")
	p := &Plugin{info: &plugintest.MockPluginInfo{CoordinatorValue: eng}}
	items := p.buildPanelItems()
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (dedup)", len(items))
	}
	if items[0].Name != "/a" || items[1].Name != "/b" || items[2].Name != "/c" {
		t.Errorf("unexpected order: %+v", items)
	}
	for _, it := range items {
		if it.Type != "command" {
			t.Errorf("item type = %q, want command", it.Type)
		}
	}
}

func TestBuildPanelItems_Limit(t *testing.T) {
	cmds := make([]string, 30)
	for i := range cmds {
		cmds[i] = "/cmd" + string(rune('a'+i%26))
	}
	eng := newEngineWithCommands(cmds...)
	p := &Plugin{info: &plugintest.MockPluginInfo{CoordinatorValue: eng}}
	items := p.buildPanelItems()
	if len(items) != maxPanelItems {
		t.Errorf("items = %d, want %d", len(items), maxPanelItems)
	}
}

func TestBuildMenu(t *testing.T) {
	eng := newEngineWithCommands("/a", "/b", "/c", "/d", "/e", "/f", "/g")
	p := &Plugin{info: &plugintest.MockPluginInfo{CoordinatorValue: eng}}
	menu := p.buildMenu()
	if menu == nil {
		t.Fatal("menu is nil")
	}
	if len(menu.Items) == 0 {
		t.Fatal("menu has no items")
	}
	first := menu.Items[0]
	if first.Type != "menu" {
		t.Errorf("first item type = %q, want menu", first.Type)
	}
	if len(first.SubMenuItems) != 5 {
		t.Errorf("sub items = %d, want 5", len(first.SubMenuItems))
	}
	// 折叠菜单子项与顶级 send_message 项的 SendMessage 必须是完整命令
	for _, it := range menu.Items {
		if it.Type == "menu" {
			for _, sub := range it.SubMenuItems {
				if !strings.HasPrefix(sub.SendMessage, "/") {
					t.Errorf("sub send_message = %q, want command", sub.SendMessage)
				}
			}
			continue
		}
		if it.SendMessage == "" {
			t.Errorf("send_message item %q has empty SendMessage", it.Name)
		}
	}
	if len(menu.Items) > maxMenuItems {
		t.Errorf("items = %d, want <= %d", len(menu.Items), maxMenuItems)
	}
}

func TestCommandInfos_ExcludesSelfAndPermissions(t *testing.T) {
	p := &Plugin{info: &plugintest.MockPluginInfo{CoordinatorValue: &fakeReader{cmds: []engine.CommandInfo{
		{Command: "/foo", Plugin: "anime"},
		{Command: "/qqpanel", Plugin: "qqpanel"},
		{Command: "/qqmenu", Plugin: "qqpanel"},
		{Command: "/perm", Plugin: "admin", Permissions: []string{"perm.list"}},
	}}}}
	got := p.commandInfos()
	if len(got) != 1 || got[0].Command != "/foo" {
		t.Errorf("commandInfos = %+v, want only /foo", got)
	}
}

func TestCommandInfos_DefaultExcludes(t *testing.T) {
	p := &Plugin{info: &plugintest.MockPluginInfo{CoordinatorValue: &fakeReader{cmds: []engine.CommandInfo{
		{Command: "/welcome", Plugin: "welcome"},
		{Command: "/help", Plugin: "help"},
		{Command: "/update", Plugin: "updater"},
	}}}}
	got := p.commandInfos()
	if len(got) != 1 || got[0].Command != "/help" {
		t.Errorf("commandInfos = %+v, want only /help", got)
	}
}

func TestCommandInfos_ConfigExcludes(t *testing.T) {
	p := &Plugin{
		info: &plugintest.MockPluginInfo{CoordinatorValue: &fakeReader{cmds: []engine.CommandInfo{
			{Command: "/weather", Plugin: "weather"},
			{Command: "/pic", Plugin: "pic"},
		}}},
		exclude: []string{"/weather"},
	}
	got := p.commandInfos()
	if len(got) != 1 || got[0].Command != "/pic" {
		t.Errorf("commandInfos = %+v, want only /pic", got)
	}
}

func TestHandlePanel_List(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	api.panelListResult = gjson.Parse(`{"records":[
		{"panel_id":"p_1","scope":"group","target_type":"all","panel":{"items":[{"name":"/a"}],"remark":"test panel"}}
	]}`)
	p := newTestPlugin(t, newEngineWithCommands("/a"))

	if err := p.handlePanel(groupCtx(api, "/qqpanel list group")); err != nil {
		t.Fatalf("handlePanel error: %v", err)
	}
	reply := lastReply(api)
	if !strings.Contains(reply, "p_1") || !strings.Contains(reply, "test panel") {
		t.Errorf("reply = %q, want panel id and remark", reply)
	}
}

func TestHandlePanel_List_Empty(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	api.panelListResult = gjson.Parse(`{"records":[],"is_end":true}`)
	p := newTestPlugin(t, newEngineWithCommands("/a"))

	if err := p.handlePanel(groupCtx(api, "/qqpanel list group")); err != nil {
		t.Fatalf("handlePanel error: %v", err)
	}
	if reply := lastReply(api); !strings.Contains(reply, "暂无指令面板") {
		t.Errorf("reply = %q, want empty notice", reply)
	}
}

func TestHandlePanel_Setup(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	api.createPanelResult = gjson.Parse(`{"panel_id":"p_new"}`)
	p := newTestPlugin(t, newEngineWithCommands("/a", "/b"))

	if err := p.handlePanel(groupCtx(api, "/qqpanel setup group")); err != nil {
		t.Fatalf("handlePanel error: %v", err)
	}
	if api.createdReq == nil {
		t.Fatal("CreatePanel not called")
	}
	if api.createdReq.Scope != "group" || api.createdReq.TargetType != "all" {
		t.Errorf("create req = %+v, want scope=group target_type=all", api.createdReq)
	}
	if len(api.createdReq.Panel.Items) != 2 {
		t.Errorf("items = %d, want 2", len(api.createdReq.Panel.Items))
	}
	if reply := lastReply(api); !strings.Contains(reply, "p_new") {
		t.Errorf("reply = %q, want panel id", reply)
	}
}

func TestHandlePanel_Setup_InvalidScope(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	p := newTestPlugin(t, newEngineWithCommands("/a"))

	if err := p.handlePanel(groupCtx(api, "/qqpanel setup guild")); err != nil {
		t.Fatalf("handlePanel error: %v", err)
	}
	if api.createdReq != nil {
		t.Error("CreatePanel should not be called for invalid scope")
	}
	if reply := lastReply(api); !strings.Contains(reply, "无效场景") {
		t.Errorf("reply = %q, want invalid scope notice", reply)
	}
}

func TestHandlePanel_Remove(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	p := newTestPlugin(t, newEngineWithCommands("/a"))

	if err := p.handlePanel(groupCtx(api, "/qqpanel rm p_9")); err != nil {
		t.Fatalf("handlePanel error: %v", err)
	}
	if api.deletedPanelID != "p_9" {
		t.Errorf("deleted panel id = %q, want p_9", api.deletedPanelID)
	}
	if reply := lastReply(api); !strings.Contains(reply, "p_9") {
		t.Errorf("reply = %q, want panel id", reply)
	}
}

func TestHandlePanel_NonQQ(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	p := newTestPlugin(t, newEngineWithCommands("/a"))
	ev := testbot.MakePlatformGroupEvent("u1", "g1", "/qqpanel list")
	ctx := eventctx.NewContextFromEvent(ev, qqplatform.NewSender(api))
	ctx.SetDispatcher(syncDispatcher{})

	if err := p.handlePanel(ctx); err != nil {
		t.Fatalf("handlePanel error: %v", err)
	}
	if reply := lastReply(api); !strings.Contains(reply, "仅支持 QQ") {
		t.Errorf("reply = %q, want QQ-only notice", reply)
	}
}

func TestHandleMenu_Sync(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	p := newTestPlugin(t, newEngineWithCommands("/a", "/b", "/c"))

	if err := p.handleMenu(c2cCtx(api, "/qqmenu synchelp")); err != nil {
		t.Fatalf("handleMenu error: %v", err)
	}
	if api.menuUpdateReq == nil || api.menuUpdateReq.Menu == nil {
		t.Fatal("UpdateMenu not called")
	}
	if len(api.menuUpdateReq.Menu.Items) == 0 {
		t.Error("menu items are empty")
	}
	if reply := lastReply(api); !strings.Contains(reply, "已同步") {
		t.Errorf("reply = %q, want sync notice", reply)
	}
}

func TestHandleMenu_Status(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	api.menuResult = gjson.Parse(`{"menu":{"items":[
		{"name":"常用指令","type":"menu","sub_menu_items":[{"name":"/help","type":"send_message","send_message":"/help"}]},
		{"name":"/ping","type":"send_message","send_message":"/ping"}
	]},"version":2}`)
	p := newTestPlugin(t, newEngineWithCommands("/a"))

	if err := p.handleMenu(c2cCtx(api, "/qqmenu status")); err != nil {
		t.Fatalf("handleMenu error: %v", err)
	}
	reply := lastReply(api)
	if !strings.Contains(reply, "版本 2") || !strings.Contains(reply, "常用指令") || !strings.Contains(reply, "/ping") {
		t.Errorf("reply = %q, want menu summary", reply)
	}
}

func TestHandleMenu_Status_Empty(t *testing.T) {
	api := &stubAPI{MockAPI: testbot.NewMockAPI()}
	api.menuResult = gjson.Parse(`{"menu":{}}`)
	p := newTestPlugin(t, newEngineWithCommands("/a"))

	if err := p.handleMenu(c2cCtx(api, "/qqmenu status")); err != nil {
		t.Fatalf("handleMenu error: %v", err)
	}
	if reply := lastReply(api); !strings.Contains(reply, "未配置") {
		t.Errorf("reply = %q, want empty menu notice", reply)
	}
}
