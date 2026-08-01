package remilia

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	netpprof "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// PprofConfig pprof 配置
type PprofConfig struct {
	// Addr 监听地址
	Addr string
	// OutputDir 性能分析文件输出目录
	OutputDir string
	// ProfileInterval 自动分析间隔
	ProfileInterval time.Duration
	// ProfileDuration 每次分析持续时间
	ProfileDuration time.Duration
	// MutexProfileFraction 互斥锁分析采样率
	MutexProfileFraction int
	// BlockProfileRate 阻塞分析采样率
	BlockProfileRate int
	// Enabled 是否启用 pprof
	Enabled bool
	// AutoProfile 是否启用自动性能分析
	AutoProfile bool
	// EnableMutex 是否启用互斥锁分析
	EnableMutex bool
	// EnableBlock 是否启用阻塞分析
	EnableBlock bool
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

type handlerEntry struct {
	path    string
	handler http.HandlerFunc
}

// PprofServer pprof 服务器
type PprofServer struct {
	server         *http.Server
	config         PprofConfig
	stopCh         chan struct{}
	stopOnce       sync.Once
	handlers       []handlerEntry
	listenerAddr   string
	listenerAddrMu sync.Mutex
}

// ListenAddr 返回 pprof 服务器实际监听的地址。
// 若 config.Addr 为 :0，ListenAddr 会在 Start() 后返回实际分配的端口。
func (p *PprofServer) ListenAddr() string {
	p.listenerAddrMu.Lock()
	defer p.listenerAddrMu.Unlock()
	return p.listenerAddr
}

// NewPprofServer 创建 pprof 服务器
func NewPprofServer(config PprofConfig) *PprofServer {
	return &PprofServer{
		config: config,
		stopCh: make(chan struct{}),
	}
}

// AddHandler 注册一个额外的 HTTP handler 到 pprof 服务器的 mux。
// 必须在 Start() 之前调用。可用于注入 /health 等管理端点。
func (p *PprofServer) AddHandler(path string, handler http.HandlerFunc) {
	p.handlers = append(p.handlers, handlerEntry{path: path, handler: handler})
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

	// 注册 pprof 路由（使用 net/http/pprof 导出的 Handler）
	// 注意：不依赖 _ "net/http/pprof" 的 init() 副作用，仅在启用时注册路由
	mux.HandleFunc("/debug/pprof/", netpprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", netpprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", netpprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", netpprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", netpprof.Trace)

	// 添加自定义端点
	mux.HandleFunc("/debug/pprof/stats", p.handleStats)
	mux.HandleFunc("/debug/pprof/snapshot", p.handleSnapshot)

	// 注册额外 handler（如 /health）
	for _, h := range p.handlers {
		mux.HandleFunc(h.path, h.handler)
	}

	p.server = &http.Server{
		Addr:    p.config.Addr,
		Handler: mux,
	}

	// 启动服务器（使用 Listen 以获取实际地址，支持 :0 随机端口）
	listener, err := net.Listen("tcp", p.config.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", p.config.Addr, err)
	}
	p.listenerAddrMu.Lock()
	p.listenerAddr = listener.Addr().String()
	p.listenerAddrMu.Unlock()
	go func() {
		logger.Infof("[Pprof] Starting pprof server on %s", p.listenerAddr)
		if err := p.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.WithError(err).Error("[Pprof] Pprof server error")
		}
	}()

	// 启动自动分析
	if p.config.AutoProfile {
		go p.autoProfile()
	}

	return nil
}

// UpdateConfig 运行时更新 pprof 配置（AutoProfile、ProfileInterval、ProfileDuration、EnableMutex、EnableBlock）。
// 注意：Enabled 和 Addr 不支持热更新。
func (p *PprofServer) UpdateConfig(cfg PprofConfig) {
	if cfg.AutoProfile {
		p.config.AutoProfile = cfg.AutoProfile
	}
	if cfg.ProfileInterval > 0 {
		p.config.ProfileInterval = cfg.ProfileInterval
	}
	if cfg.ProfileDuration > 0 {
		p.config.ProfileDuration = cfg.ProfileDuration
	}
	p.config.EnableMutex = cfg.EnableMutex
	p.config.EnableBlock = cfg.EnableBlock
	if cfg.EnableMutex {
		runtime.SetMutexProfileFraction(cfg.MutexProfileFraction)
	}
	if cfg.EnableBlock {
		runtime.SetBlockProfileRate(cfg.BlockProfileRate)
	}
	logger.WithFields(logger.Fields{
		"auto_profile":     p.config.AutoProfile,
		"profile_interval": p.config.ProfileInterval,
		"profile_duration": p.config.ProfileDuration,
	}).Info("[Pprof] Pprof config updated")
}

// Stop 停止 pprof 服务器。
//
// 可安全地重复调用：Bot.Stop 本身是幂等的，Restart 也会先后触发多次 Stop。
func (p *PprofServer) Stop(ctx context.Context) error {
	if p.server == nil {
		return nil
	}

	// 停止自动分析。
	// 必须用 sync.Once 保护：stopCh 只在 NewPprofServer 中创建、Start 不会重建，
	// 无条件 close 会在第二次 Stop（如 Restart 后再关停、或信号处理与 defer
	// 各调一次 Shutdown）时 panic("close of closed channel")，
	// 在优雅停机途中崩溃并跳过插件 Teardown。
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})

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
func (p *PprofServer) handleStats(w http.ResponseWriter, _ *http.Request) {
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
func (p *PprofServer) handleSnapshot(w http.ResponseWriter, _ *http.Request) {
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
