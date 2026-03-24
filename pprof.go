package remilia

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof" // 副作用导入：自动注册 pprof HTTP 处理器
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// PprofConfig pprof 配置
type PprofConfig struct {
	// Enabled 是否启用 pprof
	Enabled bool

	// Addr 监听地址
	Addr string

	// AutoProfile 是否启用自动性能分析
	AutoProfile bool

	// ProfileInterval 自动分析间隔
	ProfileInterval time.Duration

	// ProfileDuration 每次分析持续时间
	ProfileDuration time.Duration

	// OutputDir 性能分析文件输出目录
	OutputDir string

	// EnableMutex 是否启用互斥锁分析
	EnableMutex bool

	// EnableBlock 是否启用阻塞分析
	EnableBlock bool

	// MutexProfileFraction 互斥锁分析采样率
	MutexProfileFraction int

	// BlockProfileRate 阻塞分析采样率
	BlockProfileRate int
}

// DefaultPprofConfig 返回默认配置
func DefaultPprofConfig() PprofConfig {
	return PprofConfig{
		Enabled:              false,
		Addr:                 "localhost:9001",
		AutoProfile:          false,
		ProfileInterval:      1 * time.Hour,
		ProfileDuration:      30 * time.Second,
		OutputDir:            "./profiles",
		EnableMutex:          false,
		EnableBlock:          false,
		MutexProfileFraction: 1,
		BlockProfileRate:     1,
	}
}

// PprofServer pprof 服务器
type PprofServer struct {
	server *http.Server
	config PprofConfig
	stopCh chan struct{}
}

// NewPprofServer 创建 pprof 服务器
func NewPprofServer(config PprofConfig) *PprofServer {
	return &PprofServer{
		config: config,
		stopCh: make(chan struct{}),
	}
}

// Start 启动 pprof 服务器
func (p *PprofServer) Start() error {
	if !p.config.Enabled {
		logger.Info("[Pprof] Pprof is disabled")
		return nil
	}

	// 创建输出目录
	if p.config.AutoProfile {
		if err := os.MkdirAll(p.config.OutputDir, 0755); err != nil {
			logger.WithError(err).Error("[Pprof] Failed to create profile output directory")
			return err
		}
	}

	// 启用互斥锁分析
	if p.config.EnableMutex {
		runtime.SetMutexProfileFraction(p.config.MutexProfileFraction)
		logger.Infof("[Pprof] Mutex profiling enabled (fraction: %d)", p.config.MutexProfileFraction)
	}

	// 启用阻塞分析
	if p.config.EnableBlock {
		runtime.SetBlockProfileRate(p.config.BlockProfileRate)
		logger.Infof("[Pprof] Block profiling enabled (rate: %d)", p.config.BlockProfileRate)
	}

	// 创建 HTTP 服务器
	mux := http.NewServeMux()

	// 注册 pprof 路由
	mux.HandleFunc("/debug/pprof/", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/cmdline", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/profile", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/symbol", http.DefaultServeMux.ServeHTTP)
	mux.HandleFunc("/debug/pprof/trace", http.DefaultServeMux.ServeHTTP)

	// 添加自定义端点
	mux.HandleFunc("/debug/pprof/stats", p.handleStats)
	mux.HandleFunc("/debug/pprof/snapshot", p.handleSnapshot)

	p.server = &http.Server{
		Addr:    p.config.Addr,
		Handler: mux,
	}

	// 启动服务器
	go func() {
		logger.Infof("[Pprof] Starting pprof server on %s", p.config.Addr)
		if err := p.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("[Pprof] Pprof server error")
		}
	}()

	// 启动自动分析
	if p.config.AutoProfile {
		go p.autoProfile()
	}

	return nil
}

// Stop 停止 pprof 服务器
func (p *PprofServer) Stop(ctx context.Context) error {
	if p.server == nil {
		return nil
	}

	// 停止自动分析
	close(p.stopCh)

	// 关闭服务器
	logger.Info("[Pprof] Stopping pprof server")
	return p.server.Shutdown(ctx)
}

// autoProfile 自动生成性能分析文件
func (p *PprofServer) autoProfile() {
	ticker := time.NewTicker(p.config.ProfileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.captureProfiles()
		case <-p.stopCh:
			return
		}
	}
}

// captureProfiles 捕获所有类型的性能分析
func (p *PprofServer) captureProfiles() {
	timestamp := time.Now().Format("20060102_150405")

	// CPU 性能分析
	if err := p.captureCPUProfile(timestamp); err != nil {
		logger.WithError(err).Error("[Pprof] Failed to capture CPU profile")
	}

	// 堆内存分析
	if err := p.captureHeapProfile(timestamp); err != nil {
		logger.WithError(err).Error("[Pprof] Failed to capture heap profile")
	}

	// Goroutine 分析
	if err := p.captureGoroutineProfile(timestamp); err != nil {
		logger.WithError(err).Error("[Pprof] Failed to capture goroutine profile")
	}

	// 互斥锁分析（如已启用）
	if p.config.EnableMutex {
		if err := p.captureMutexProfile(timestamp); err != nil {
			logger.WithError(err).Error("[Pprof] Failed to capture mutex profile")
		}
	}

	// 阻塞分析（如已启用）
	if p.config.EnableBlock {
		if err := p.captureBlockProfile(timestamp); err != nil {
			logger.WithError(err).Error("[Pprof] Failed to capture block profile")
		}
	}

	logger.Infof("[Pprof] Captured profiles: %s", timestamp)
}

// captureCPUProfile 捕获 CPU profile
func (p *PprofServer) captureCPUProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/cpu_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := pprof.StartCPUProfile(f); err != nil {
		return err
	}

	// 使用 select 替代裸 time.Sleep，支持 Stop() 时提前中止 CPU 采集
	timer := time.NewTimer(p.config.ProfileDuration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-p.stopCh:
	}
	pprof.StopCPUProfile()

	return nil
}

// captureHeapProfile 捕获堆内存 profile
func (p *PprofServer) captureHeapProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/heap_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	runtime.GC() // 触发 GC 获取准确数据
	return pprof.WriteHeapProfile(f)
}

// captureGoroutineProfile 捕获 goroutine profile
func (p *PprofServer) captureGoroutineProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/goroutine_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return pprof.Lookup("goroutine").WriteTo(f, 0)
}

// captureMutexProfile 捕获互斥锁 profile
func (p *PprofServer) captureMutexProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/mutex_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return pprof.Lookup("mutex").WriteTo(f, 0)
}

// captureBlockProfile 捕获阻塞 profile
func (p *PprofServer) captureBlockProfile(timestamp string) error {
	filename := fmt.Sprintf("%s/block_%s.prof", p.config.OutputDir, timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return pprof.Lookup("block").WriteTo(f, 0)
}

// handleStats 处理统计信息请求
func (p *PprofServer) handleStats(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Fprintf(w, "Runtime Statistics\n")
	fmt.Fprintf(w, "==================\n\n")
	fmt.Fprintf(w, "Goroutines: %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "NumCPU: %d\n", runtime.NumCPU())
	fmt.Fprintf(w, "GOMAXPROCS: %d\n\n", runtime.GOMAXPROCS(0))

	fmt.Fprintf(w, "Memory Statistics\n")
	fmt.Fprintf(w, "=================\n\n")
	fmt.Fprintf(w, "Alloc: %d MB\n", m.Alloc/1024/1024)
	fmt.Fprintf(w, "TotalAlloc: %d MB\n", m.TotalAlloc/1024/1024)
	fmt.Fprintf(w, "Sys: %d MB\n", m.Sys/1024/1024)
	fmt.Fprintf(w, "NumGC: %d\n", m.NumGC)
	fmt.Fprintf(w, "GCCPUFraction: %.4f\n", m.GCCPUFraction)
	fmt.Fprintf(w, "HeapAlloc: %d MB\n", m.HeapAlloc/1024/1024)
	fmt.Fprintf(w, "HeapSys: %d MB\n", m.HeapSys/1024/1024)
	fmt.Fprintf(w, "HeapIdle: %d MB\n", m.HeapIdle/1024/1024)
	fmt.Fprintf(w, "HeapInuse: %d MB\n", m.HeapInuse/1024/1024)
}

// handleSnapshot 处理快照请求
func (p *PprofServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	timestamp := time.Now().Format("20060102_150405")

	go func() {
		p.captureProfiles()
	}()

	fmt.Fprintf(w, "Snapshot captured: %s\n", timestamp)
	fmt.Fprintf(w, "Files will be saved to: %s\n", p.config.OutputDir)
}

// CaptureTrace 捕获执行追踪
//
// ctx 可用于提前中止追踪（例如收到停止信号时）。
// 传入 context.Background() 则等待完整的 duration。
func CaptureTrace(ctx context.Context, duration time.Duration, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := trace.Start(f); err != nil {
		return err
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	trace.Stop()

	return nil
}
