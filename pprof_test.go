package remilia_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startPprofOnDynamicPort 在随机端口上启动 pprof 服务器，返回 server 和实际监听地址。
func startPprofOnDynamicPort(t *testing.T, cfg remilia.PprofConfig) (*remilia.PprofServer, string) {
	t.Helper()
	cfg.Addr = "127.0.0.1:0"
	server := remilia.NewPprofServer(cfg)
	require.NotNil(t, server)
	err := server.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	})
	addr := server.ListenAddr()
	require.NotEmpty(t, addr, "PprofServer should report its listen address")
	waitPprofServer(t, addr)
	return server, addr
}

func waitPprofServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pprof server did not start on %s within timeout", addr)
}

func TestPprofServer(t *testing.T) {
	cfg := remilia.PprofConfig{
		Enabled:     true,
		AutoProfile: false,
	}
	_, addr := startPprofOnDynamicPort(t, cfg)
	assert.NotEmpty(t, addr)
}

func TestPprofServerDisabled(t *testing.T) {
	config := remilia.PprofConfig{
		Enabled: false,
	}
	server := remilia.NewPprofServer(config)
	require.NotNil(t, server)
	err := server.Start()
	assert.NoError(t, err)
	err = server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestPprofAutoProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping auto profile test in short mode")
	}

	tmpDir := t.TempDir()
	cfg := remilia.PprofConfig{
		Enabled:         true,
		AutoProfile:     true,
		ProfileInterval: 2 * time.Second,
		ProfileDuration: 100 * time.Millisecond,
		OutputDir:       tmpDir,
	}
	_, _ = startPprofOnDynamicPort(t, cfg)

	time.Sleep(3 * time.Second)

	files, err := os.ReadDir(tmpDir)
	assert.NoError(t, err)
	assert.Greater(t, len(files), 0, "Should have generated profile files")
}

func TestPprofUpdateConfig(t *testing.T) {
	cfg := remilia.PprofConfig{
		Enabled:         true,
		AutoProfile:     false,
		EnableMutex:     false,
		EnableBlock:     false,
		ProfileInterval: 1 * time.Hour,
		ProfileDuration: 30 * time.Second,
	}
	server, addr := startPprofOnDynamicPort(t, cfg)

	server.UpdateConfig(remilia.PprofConfig{
		AutoProfile:          true,
		ProfileInterval:      30 * time.Minute,
		ProfileDuration:      10 * time.Second,
		EnableMutex:          true,
		EnableBlock:          true,
		MutexProfileFraction: 1,
		BlockProfileRate:     1,
	})

	dial, err := net.DialTimeout("tcp", addr, time.Second)
	assert.NoError(t, err)
	dial.Close()
}

func TestPprofMutexAndBlock(t *testing.T) {
	cfg := remilia.PprofConfig{
		Enabled:              true,
		EnableMutex:          true,
		EnableBlock:          true,
		MutexProfileFraction: 1,
		BlockProfileRate:     1,
	}
	_, addr := startPprofOnDynamicPort(t, cfg)
	dial, err := net.DialTimeout("tcp", addr, time.Second)
	assert.NoError(t, err)
	dial.Close()
}

func TestCaptureTrace(t *testing.T) {
	tmpDir := t.TempDir()
	filename := fmt.Sprintf("%s/trace.out", tmpDir)

	err := remilia.CaptureTrace(context.Background(), 100*time.Millisecond, filename)
	assert.NoError(t, err)

	_, err = os.Stat(filename)
	assert.NoError(t, err)
}
