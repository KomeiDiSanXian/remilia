package remilia_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPprofServer 测试 pprof 服务器
func TestPprofServer(t *testing.T) {
	config := remilia.PprofConfig{
		Enabled:     true,
		Addr:        "localhost:19001",
		AutoProfile: false,
	}

	server := remilia.NewPprofServer(config)
	require.NotNil(t, server)

	// 启动服务器
	err := server.Start()
	assert.NoError(t, err)

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 停止服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = server.Stop(ctx)
	assert.NoError(t, err)
}

// TestPprofServerDisabled 测试禁用的 pprof 服务器
func TestPprofServerDisabled(t *testing.T) {
	config := remilia.PprofConfig{
		Enabled: false,
	}

	server := remilia.NewPprofServer(config)
	require.NotNil(t, server)

	// 启动应该成功但不做任何事
	err := server.Start()
	assert.NoError(t, err)

	// 停止也应该成功
	err = server.Stop(context.Background())
	assert.NoError(t, err)
}

// TestPprofAutoProfile 测试自动性能分析
func TestPprofAutoProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping auto profile test in short mode")
	}

	tmpDir := t.TempDir()
	config := remilia.PprofConfig{
		Enabled:         true,
		Addr:            "localhost:19002",
		AutoProfile:     true,
		ProfileInterval: 2 * time.Second,
		ProfileDuration: 100 * time.Millisecond,
		OutputDir:       tmpDir,
	}

	server := remilia.NewPprofServer(config)
	require.NotNil(t, server)

	// 启动服务器
	err := server.Start()
	assert.NoError(t, err)

	// 等待至少一次自动分析
	time.Sleep(3 * time.Second)

	// 停止服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = server.Stop(ctx)
	assert.NoError(t, err)

	// 验证生成了 profile 文件
	files, err := os.ReadDir(tmpDir)
	assert.NoError(t, err)
	assert.Greater(t, len(files), 0, "Should have generated profile files")
}

// TestCaptureTrace 测试执行追踪捕获
func TestCaptureTrace(t *testing.T) {
	tmpDir := t.TempDir()
	filename := tmpDir + "/trace.out"

	err := remilia.CaptureTrace(100*time.Millisecond, filename)
	assert.NoError(t, err)

	// 验证文件创建
	_, err = os.Stat(filename)
	assert.NoError(t, err)
}

// TestPprofMutexAndBlock 测试互斥锁和阻塞分析
func TestPprofMutexAndBlock(t *testing.T) {
	config := remilia.PprofConfig{
		Enabled:              true,
		Addr:                 "localhost:19003",
		EnableMutex:          true,
		EnableBlock:          true,
		MutexProfileFraction: 1,
		BlockProfileRate:     1,
	}

	server := remilia.NewPprofServer(config)
	require.NotNil(t, server)

	// 启动服务器
	err := server.Start()
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// 停止服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = server.Stop(ctx)
	assert.NoError(t, err)
}
