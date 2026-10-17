package plugin

// lifecycle_metagm_test.go — 元数据 goroutine 管理器重建的回归测试
//
// 回归背景：lifecycleController.metaGM 是普通字段，Shutdown（停机）与
// ensureMetaGM（StartAll 重启）交叠时既构成数据竞争，重建也可能被停机覆盖。
// 修复后字段为 atomic.Pointer，重建走 CAS。

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycleController_MetaGMRestartRace(t *testing.T) {
	lc := newLifecycleController(&Manager{})

	// Shutdown（停机路径）与 ensureMetaGM（StartAll 重启路径）交叠：
	// 旧实现直接读写 metaGM 字段，-race 会报数据竞争。
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			lc.ensureMetaGM()
		}()
		go func() {
			defer wg.Done()
			lc.Shutdown()
		}()
	}
	wg.Wait()

	require.NotNil(t, lc.metaManager(), "重建后必须存在元数据管理器")

	// 最后一次 Shutdown 可能落在重建之前，重启一次即可恢复；语义与
	// ensureMetaGM 文档一致（Stop → Start 循环可重启）。
	lc.ensureMetaGM()
	assert.False(t, lc.metaManager().isStopped(), "重启后的元数据管理器应可用")

	lc.Shutdown()
	assert.True(t, lc.metaManager().isStopped())
}
