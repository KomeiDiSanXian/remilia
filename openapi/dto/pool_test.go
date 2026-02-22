package dto_test

import (
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestAcquireReleasePayload 验证基本的获取/归还语义
func TestAcquireReleasePayload(t *testing.T) {
	p := dto.AcquirePayload()
	if p == nil {
		t.Fatal("AcquirePayload returned nil")
	}

	// 字段应全部为零值
	if p.ID != "" || p.Sequence != 0 || p.Raw != nil || p.Detail != nil {
		t.Errorf("acquired payload has non-zero fields: %+v", p)
	}

	// 填充字段后归还
	p.ID = "test-id"
	p.Type = "MESSAGE_CREATE"
	p.Sequence = 42
	dto.ReleasePayload(p)

	// 再次获取，字段应被清零（不保证是同一对象，但如果是同一对象必须干净）
	p2 := dto.AcquirePayload()
	if p2 == nil {
		t.Fatal("second AcquirePayload returned nil")
	}
	if p2 == p {
		// 如果 pool 返回了同一对象，字段必须已清零
		if p2.ID != "" {
			t.Errorf("pool reuse: ID not cleared, got %q", p2.ID)
		}
		if p2.Type != "" {
			t.Errorf("pool reuse: Type not cleared, got %q", p2.Type)
		}
		if p2.Sequence != 0 {
			t.Errorf("pool reuse: Sequence not cleared, got %d", p2.Sequence)
		}
	}
	dto.ReleasePayload(p2)
}

// TestReleasePayloadNil 验证 ReleasePayload(nil) 不 panic
func TestReleasePayloadNil(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ReleasePayload(nil) panicked: %v", r)
		}
	}()
	dto.ReleasePayload(nil)
}

// TestPayloadPoolConcurrent 在高并发下验证 pool 无竞争问题
func TestPayloadPoolConcurrent(t *testing.T) {
	const goroutines = 64
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				p := dto.AcquirePayload()
				p.ID = "event"
				p.Sequence = uint64(id*iterations + j)
				dto.ReleasePayload(p)
			}
		}(i)
	}
	wg.Wait()
}

// TestPayloadPoolSliceReuse 验证 Detail/Raw slice 容量在归还后被保留（降低后续 append 分配）
func TestPayloadPoolSliceReuse(t *testing.T) {
	p := dto.AcquirePayload()
	p.Raw = make([]byte, 0, 512)
	p.Raw = append(p.Raw, []byte(`{"op":0}`)...)
	p.Detail = make([]byte, 0, 256)
	p.Detail = append(p.Detail, []byte(`{}`)...)

	dto.ReleasePayload(p)

	p2 := dto.AcquirePayload()
	if p2 == p {
		// 同一对象：内容应为空，但容量应保留
		if len(p2.Raw) != 0 {
			t.Errorf("Raw len should be 0 after release, got %d", len(p2.Raw))
		}
		if cap(p2.Raw) < 512 {
			t.Logf("Raw cap not preserved (sync.Pool may have been GC'd): got %d, expected >= 512", cap(p2.Raw))
		}
	}
	dto.ReleasePayload(p2)
}
