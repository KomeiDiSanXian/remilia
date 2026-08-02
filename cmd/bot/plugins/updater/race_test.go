package updater

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// TestStateStoreConcurrent 并发读写状态存储（race detector 下运行）。
func TestStateStoreConcurrent(t *testing.T) {
	store := newStateStore(t.TempDir())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				store.update(func(st *updaterState) {
					st.LastVersion = fmt.Sprintf("1.%d.%d", n, j)
					st.LastCheck = st.LastCheck.Add(1)
				})
				_ = store.load()
			}
		}(i)
	}
	wg.Wait()

	st := store.load()
	if st.LastVersion == "" {
		t.Error("状态丢失")
	}
}

// TestPluginSingleFlight 单飞标志：锁被持有期间其他并发触发必须被拒绝。
func TestPluginSingleFlight(t *testing.T) {
	p := newTestPlugin(t)

	acquired := make(chan struct{})
	release := make(chan struct{})
	var rejected atomic.Int32

	go func() {
		p.updating.Store(true) // 模拟一个更新流程持有单飞锁
		close(acquired)
		<-release
		p.updating.Store(false)
	}()
	<-acquired

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !p.updating.CompareAndSwap(false, true) {
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()
	close(release)

	if got := rejected.Load(); got != 16 {
		t.Errorf("锁持有期间应拒绝全部 16 个并发触发，实际拒绝 %d", got)
	}
}

// TestPluginAutoCheckFlag 自动检查开关的并发读写（race detector 下运行）。
func TestPluginAutoCheckFlag(t *testing.T) {
	p := newTestPlugin(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				p.autoCheck.Store(j%2 == 0)
				_ = p.autoCheck.Load()
			}
		}()
	}
	wg.Wait()
}
