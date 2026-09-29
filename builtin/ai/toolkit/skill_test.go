package toolkit

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSkillKey(t *testing.T) {
	key := skillKey("owner1", "skill1")
	expected := "owner1\x00skill1"
	if key != expected {
		t.Errorf("expected %q, got %q", expected, key)
	}
}

// TestSkillRegistryAddCapped 冻结：数量上限与注册在同一把写锁内完成，
// 达到上限返回 ErrOwnerSkillLimit；不同所有者各自计数；max<=0 不限。
func TestSkillRegistryAddCapped(t *testing.T) {
	r := NewSkillRegistry()
	if err := r.AddCapped(Skill{OwnerID: "u1", Name: "a"}, 1); err != nil {
		t.Fatalf("首次注册应成功: %v", err)
	}
	if err := r.AddCapped(Skill{OwnerID: "u1", Name: "b"}, 1); !errors.Is(err, ErrOwnerSkillLimit) {
		t.Fatalf("超出上限应返回 ErrOwnerSkillLimit，得到 %v", err)
	}
	if err := r.AddCapped(Skill{OwnerID: "u2", Name: "c"}, 1); err != nil {
		t.Fatalf("不同所有者各自计数，应成功: %v", err)
	}
	if err := r.AddCapped(Skill{OwnerID: "u1", Name: "d"}, 0); err != nil {
		t.Fatalf("max=0 应不限: %v", err)
	}
}

// TestSkillRegistryAddCappedConcurrent 并发注册时恰好只有 max 个成功：
// "先计数再 Add" 的 TOCTOU 会让多个调用一起通过检查而突破上限。
func TestSkillRegistryAddCappedConcurrent(t *testing.T) {
	r := NewSkillRegistry()
	const (
		max = 5
		n   = 40
	)
	var (
		wg    sync.WaitGroup
		ok    atomic.Int64
		limit atomic.Int64
	)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := r.AddCapped(Skill{OwnerID: "u1", Name: fmt.Sprintf("s%d", i)}, max)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrOwnerSkillLimit):
				limit.Add(1)
			default:
				t.Errorf("意外错误: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if got := ok.Load(); got != max {
		t.Fatalf("并发注册应恰好成功 %d 个，实际 %d", max, got)
	}
	if got := limit.Load(); got != n-max {
		t.Fatalf("其余应全部返回上限错误（%d），实际 %d", n-max, got)
	}
	if got := len(r.ListByOwner("u1")); got != max {
		t.Fatalf("注册表实际条目应为 %d，得到 %d", max, got)
	}
}
