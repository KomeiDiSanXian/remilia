package updater

import (
	"os"
	"testing"
)

// TestCurrentConsoleMode 控制台策略取值优先级：环境变量（子进程侧）> 配置值。
func TestCurrentConsoleMode(t *testing.T) {
	oldEnv, hadEnv := os.LookupEnv(envChildConsole)
	oldMode := childConsoleMode
	defer func() {
		if hadEnv {
			os.Setenv(envChildConsole, oldEnv)
		} else {
			os.Unsetenv(envChildConsole)
		}
		childConsoleMode = oldMode
	}()

	os.Unsetenv(envChildConsole)
	childConsoleMode = ""
	if m := currentConsoleMode(); m != "" {
		t.Errorf("no config, want empty, got %q", m)
	}

	childConsoleMode = "new"
	if m := currentConsoleMode(); m != "new" {
		t.Errorf("config=new, want \"new\", got %q", m)
	}

	os.Setenv(envChildConsole, "new")
	childConsoleMode = ""
	if m := currentConsoleMode(); m != "new" {
		t.Errorf("env=new overrides empty config, want \"new\", got %q", m)
	}

	os.Setenv(envChildConsole, "")
	childConsoleMode = "new"
	if m := currentConsoleMode(); m != "new" {
		t.Errorf("empty env falls back to config, want \"new\", got %q", m)
	}
}
