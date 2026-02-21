package audit

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteLoop_DrainOnStop 验证修复 #3：writeLoop 关闭时安全排空 channel，不死锁
func TestWriteLoop_DrainOnStop(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:       true,
		OutputFile:    filepath.Join(dir, "test_drain.log"),
		BufferSize:    200,
		FlushInterval: 10 * time.Second, // 长间隔确保不触发定时刷新
		MinLevel:      LevelInfo,
		AsyncWrite:    true,
	}
	l, err := NewLogger(cfg)
	require.NoError(t, err)
	defer os.Remove(cfg.OutputFile)

	// 并发写入若干条目
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.Info(ActionUserLogin, "test-user", "concurrent write")
		}(i)
	}
	wg.Wait()

	// Close 应在合理时间内完成（不死锁）
	done := make(chan error, 1)
	go func() {
		done <- l.Close()
	}()

	select {
	case err := <-done:
		assert.NoError(t, err, "Close should not error")
	case <-time.After(3 * time.Second):
		t.Fatal("Close deadlocked (writeLoop drain blocked)")
	}

	// 验证日志文件已写入
	stat, err := os.Stat(cfg.OutputFile)
	require.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(0), "Log file should have content")
}

// TestWriteLoop_DrainOnStop_ConcurrentProducers 模拟关闭时有并发生产者
func TestWriteLoop_DrainOnStop_ConcurrentProducers(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Enabled:       true,
		OutputFile:    filepath.Join(dir, "test_drain2.log"),
		BufferSize:    50,
		FlushInterval: 10 * time.Second,
		MinLevel:      LevelInfo,
		AsyncWrite:    true,
	}
	l, err := NewLogger(cfg)
	require.NoError(t, err)
	defer os.Remove(cfg.OutputFile)

	// 启动持续写入的 goroutine
	stopWriting := make(chan struct{})
	var writeWg sync.WaitGroup
	writeWg.Go(func() {
		for {
			select {
			case <-stopWriting:
				return
			default:
				l.Info(ActionUserLogin, "user", "background write")
				time.Sleep(time.Millisecond)
			}
		}
	})

	// 写入一段时间后关闭
	time.Sleep(50 * time.Millisecond)
	close(stopWriting)
	writeWg.Wait()

	// Close 应该正常完成
	done := make(chan error, 1)
	go func() {
		done <- l.Close()
	}()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Close deadlocked with concurrent producers")
	}
}
