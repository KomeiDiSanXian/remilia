package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFrameworkContextIsAliased 守卫"框架事件上下文一律以 eventctx 别名导入"。
//
// 框架的 core/context 与标准库 context 都叫 context。历史上两种导入并存：
// 有的文件写 eventctx，有的直接叫 context，于是同名标识符的含义取决于打开
// 哪个文件——`ctx *context.Context` 到底是事件上下文还是标准库上下文，
// 读者只能靠猜。统一用别名后，两者同时出现的文件也能一眼区分。
func TestFrameworkContextIsAliased(t *testing.T) {
	const importPath = `"github.com/KomeiDiSanXian/remilia/core/context"`
	const aliased = `eventctx ` + importPath

	root := "."
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(root, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(b)
		if !strings.Contains(src, importPath) {
			continue
		}
		if !strings.Contains(src, aliased) {
			t.Errorf("%s imports the framework context without the eventctx alias", e.Name())
		}
	}
}

// TestSessionParamIsNotShadowingPackage 守卫"形参不要遮蔽包名"。
//
// `session *session.Session` 让函数体里再出现 session.X 时完全无法判断
// 那是形参的字段还是包级函数；统一用 sess 之后这两类引用在文本上就分开了。
func TestSessionParamIsNotShadowingPackage(t *testing.T) {
	const bad = "session *session.Session"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(".", e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(b), bad) {
			t.Errorf("%s declares a parameter that shadows the session package", e.Name())
		}
	}
}
