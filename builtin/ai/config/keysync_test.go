package config

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"testing"
)

// TestYAMLTagsMatchReadKeys 守卫"配置项文档与实际读取一致"。
//
// Config 上的 97 个 yaml tag 从未被运行时使用（Load 走 ConfigReader 的
// Get/GetString/...），tag 只是配置项文档。两份独立文本最容易悄悄分叉：
// 加了字段忘了读、改了 key 忘了改 tag。这里用源码扫描把两边对齐。
func TestYAMLTagsMatchReadKeys(t *testing.T) {
	tags := yamlTags(t)
	keys := readKeys(t)

	if len(tags) == 0 || len(keys) == 0 {
		t.Fatalf("failed to collect tags/keys: %d tags, %d keys", len(tags), len(keys))
	}

	for _, tag := range tags {
		if _, ok := keys[tag]; !ok {
			t.Errorf("yaml tag %q is documented but never read by Load", tag)
		}
	}
	for key := range keys {
		if !containsString(tags, key) {
			t.Errorf("key %q is read by Load but has no yaml tag on Config", key)
		}
	}
}

// TestEveryFieldHasYAMLTags 守卫生成文档时不漏字段。
func TestEveryFieldHasYAMLTags(t *testing.T) {
	typ := reflect.TypeFor[Config]()
	for f := range typ.Fields() {
		if f.Tag.Get("yaml") == "" {
			t.Errorf("Config field %s has no yaml tag", f.Name)
		}
	}
}

func yamlTags(t *testing.T) []string {
	t.Helper()
	src := readSource(t, "config.go")
	re := regexp.MustCompile("yaml:\"([a-z0-9_]+)\"")
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		seen[m[1]] = true
	}
	return sortedKeys(seen)
}

// readKeys 扫 load.go 里所有"读取配置 key"的调用点，返回 key 集合。
func readKeys(t *testing.T) map[string]struct{} {
	t.Helper()
	src := readSource(t, "load.go")
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:Get|GetString|GetInt|GetBool|GetDuration)\("([a-z0-9_]+)"`),
		regexp.MustCompile(`(?:intOption|floatOption|present)\(ctx, "([a-z0-9_]+)"`),
		// stringList(ctx, "key", ...) 以及表驱动里的字面量 key。
		regexp.MustCompile(`stringList\(ctx, "([a-z0-9_]+)"`),
	}
	seen := map[string]struct{}{}
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			seen[m[1]] = struct{}{}
		}
	}
	// 表驱动列表（exec_allowlist 等）以 `"key": &cfg.Field` 形式出现。
	mapRe := regexp.MustCompile(`"([a-z0-9_]+)":\s*&cfg\.`)
	for _, m := range mapRe.FindAllStringSubmatch(src, -1) {
		seen[m[1]] = struct{}{}
	}
	return seen
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsString(list []string, want string) bool {
	return slices.Contains(list, want)
}
