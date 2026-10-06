package dealornodeal

import (
	"testing"
	"time"
)

func TestParseScope(t *testing.T) {
	ok := map[string]Scope{
		"user": ScopeUser, "solo": ScopeUser, "private": ScopeUser,
		"group": ScopeGroup, "chat": ScopeGroup, "vote": ScopeGroup,
	}
	for in, want := range ok {
		got, valid := ParseScope(in)
		if !valid || got != want {
			t.Errorf("ParseScope(%q) = %v/%v, 期望 %v/true", in, got, valid, want)
		}
	}
	if _, valid := ParseScope("nope"); valid {
		t.Error("未知维度不应解析成功")
	}
	if ScopeUser.String() != "user" || ScopeGroup.String() != "group" {
		t.Error("Scope.String 输出错误")
	}
}

func TestSessionKey(t *testing.T) {
	user := SessionKey("qq", "chat1", "u1", ScopeUser)
	group := SessionKey("qq", "chat1", "u1", ScopeGroup)
	if user == group {
		t.Fatal("user 与 group 维度键应不同")
	}
	if group != SessionKey("qq", "chat1", "u2", ScopeGroup) {
		t.Fatal("group 维度应忽略用户 ID")
	}
	if user != SessionKey("qq", "chat1", "u1", ScopeUser) {
		t.Fatal("user 维度键应稳定")
	}
}

func TestSessionStore_CRUD(t *testing.T) {
	s := NewSessionStore(0)
	if s.ttl != 30*time.Minute {
		t.Fatalf("默认 TTL = %v, 期望 30m", s.ttl)
	}
	g := NewGame(Options{})
	s.Put("k", g)
	if s.Len() != 1 {
		t.Fatalf("Len = %d, 期望 1", s.Len())
	}
	if got, ok := s.Get("k"); !ok || got != g {
		t.Fatal("Get 未返回写入的对局")
	}
	s.Delete("k")
	if s.Len() != 0 {
		t.Fatal("Delete 后 Len 应为 0")
	}
}

func TestSessionStore_FindPrefersActive(t *testing.T) {
	s := NewSessionStore(0)
	userGame := NewGame(Options{Scope: ScopeUser})
	groupGame := NewGame(Options{Scope: ScopeGroup})
	s.Put(SessionKey("p", "c", "u", ScopeUser), userGame)
	s.Put(SessionKey("p", "c", "u", ScopeGroup), groupGame)
	if got, ok := s.Find("p", "c", "u"); !ok || got != userGame {
		t.Fatal("应优先返回用户维度对局")
	}
	userGame.Finished = true
	if got, ok := s.Find("p", "c", "u"); !ok || got != groupGame {
		t.Fatal("用户维度已结束时应回退到群维度对局")
	}
}

func TestSessionStore_UpdateFound(t *testing.T) {
	s := NewSessionStore(0)
	g := NewGame(Options{Scope: ScopeUser})
	s.Put(SessionKey("p", "c", "u", ScopeUser), g)
	called := false
	if !s.UpdateFound("p", "c", "u", func(gg *Game) {
		called = true
		gg.Round = 3
	}) {
		t.Fatal("UpdateFound 应命中")
	}
	if !called || g.Round != 3 {
		t.Fatal("UpdateFound 未执行回调")
	}
	if s.UpdateFound("p", "c", "nobody", func(*Game) {}) {
		t.Fatal("不存在的对局不应命中")
	}
}

func TestSessionStore_Sweep(t *testing.T) {
	s := NewSessionStore(time.Minute)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	fresh := NewGame(Options{Now: now})
	stale := NewGame(Options{Now: now})
	stale.UpdatedAt = now.Add(-2 * time.Minute)
	s.Put("fresh", fresh)
	s.Put("stale", stale)
	if removed := s.Sweep(); removed != 1 {
		t.Fatalf("回收数量 = %d, 期望 1", removed)
	}
	if _, ok := s.Get("fresh"); !ok {
		t.Fatal("未超时的对局不应被回收")
	}
	if _, ok := s.Get("stale"); ok {
		t.Fatal("超时的对局应被回收")
	}
}
