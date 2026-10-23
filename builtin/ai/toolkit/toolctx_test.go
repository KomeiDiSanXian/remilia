package toolkit

import "testing"

func TestToolSourceFromInvocation(t *testing.T) {
	inv := &Invocation{Source: ToolSource{
		UserID:   "u1",
		ChatID:   "g1",
		Platform: "qq",
		IsGroup:  true,
	}}
	src, ok := ToolSourceFromInvocation(inv)
	if !ok {
		t.Fatal("应能提取会话源信息")
	}
	if src.UserID != "u1" || src.ChatID != "g1" || src.Platform != "qq" || !src.IsGroup {
		t.Errorf("字段不匹配: %+v", src)
	}

	// nil Invocation 返回 false
	if _, ok := ToolSourceFromInvocation(nil); ok {
		t.Error("nil Invocation 不应返回 ok")
	}
}

// testPort 只用于验证端口的挂载与隔离，不依赖任何上层包。
type testPort struct{ Value string }

// TestWithSkillClonesPorts 校验技能副本不会污染父调用：在副本上 SetPort
// 只影响副本，兄弟技能也各自独立。
func TestWithSkillClonesPorts(t *testing.T) {
	parent := &Invocation{}
	parent.SetPort[testPort](testPort{Value: "parent"})

	child := parent.WithSkill("child")
	child.SetPort[testPort](testPort{Value: "child"})

	if got, _ := parent.Port[testPort](); got.Value != "parent" {
		t.Fatalf("父 Invocation 的端口被污染: %+v", got)
	}
	if got, _ := child.Port[testPort](); got.Value != "child" {
		t.Fatalf("子 Invocation 的端口未生效: %+v", got)
	}
	if !child.InSkill("child") || parent.InSkill("child") {
		t.Fatalf("SkillPath 隔离失败: parent=%v child=%v", parent.SkillPath, child.SkillPath)
	}
}
