package catalog

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// TestSnapshotIsImmutableAndVersioned 锁定快照的核心契约：带代数、值语义、
// 不受构造后对输入的修改影响。选择层据此判断能否复用缓存。
func TestSnapshotIsImmutableAndVersioned(t *testing.T) {
	src := []toolkit.Action{
		toolkit.ActionOf(toolkit.Tool{Name: "b"}),
		toolkit.ActionOf(toolkit.Tool{Name: "a"}),
	}
	snap := NewSnapshot(7, src)

	if snap.Generation() != 7 {
		t.Fatalf("Generation() = %d, want 7", snap.Generation())
	}
	if snap.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", snap.Len())
	}

	// 修改输入不得影响快照（值语义）。
	src[0] = toolkit.ActionOf(toolkit.Tool{Name: "mutated"})
	if _, ok := snap.Lookup("b"); !ok {
		t.Error("snapshot must not observe later mutation of the input slice")
	}
	if _, ok := snap.Lookup("mutated"); ok {
		t.Error("snapshot leaked a mutated action")
	}

	// 取出的 Actions 也是副本：再次修改不改变快照。
	got := snap.Actions()
	got[1] = toolkit.ActionOf(toolkit.Tool{Name: "x"})
	if _, ok := snap.Lookup("a"); !ok {
		t.Error("Actions() must return a defensive copy")
	}
}

// TestSnapshotSortedNamesKeepsDeterministicOrder 名称列表按升序，便于稳定断言。
func TestSnapshotSortedNamesKeepsDeterministicOrder(t *testing.T) {
	snap := NewSnapshot(1, []toolkit.Action{
		toolkit.ActionOf(toolkit.Tool{Name: "c"}),
		toolkit.ActionOf(toolkit.Tool{Name: "a"}),
		toolkit.ActionOf(toolkit.Tool{Name: "b"}),
	})
	names := snap.SortedNames()
	want := []string{"a", "b", "c"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("SortedNames() = %v, want %v", names, want)
		}
	}
}

// TestZeroSnapshotIsEmpty 零值可用：空目录、代数为零。
func TestZeroSnapshotIsEmpty(t *testing.T) {
	var snap CatalogSnapshot
	if snap.Len() != 0 || snap.Generation() != 0 {
		t.Fatalf("zero snapshot must be empty, got len=%d gen=%d", snap.Len(), snap.Generation())
	}
	if _, ok := snap.Lookup("anything"); ok {
		t.Error("zero snapshot must not contain anything")
	}
}
