# Remilia 性能分析指南

> 版本: v1.2.1+  
> 最后更新: 2025-12-07

---

## 📖 概述

本文档介绍如何使用 Remilia 的内置性能分析工具和 Go 标准库的 pprof 进行性能分析和优化。

---

## 🎯 性能分析目标

性能分析可以帮助你：

- 🔍 **识别 CPU 瓶颈**：找出消耗 CPU 最多的函数
- 💾 **分析内存使用**：检测内存泄漏和高内存消耗
- 🧵 **检测 Goroutine 泄漏**：发现未正常退出的 goroutine
- ⏱️ **优化响应时间**：减少事件处理延迟
- 🔒 **分析锁竞争**：识别并发瓶颈

---

## 🔧 启用 pprof

### 1. 使用内置 pprof 支持

Remilia 提供了开箱即用的 pprof 支持：

```go
package main

import (
    "github.com/KomeiDiSanXian/remilia"
    _ "github.com/KomeiDiSanXian/remilia/pprof" // 导入即启用
)

func main() {
    engine := remilia.NewEngine()
    
    // pprof 服务自动启动在 :6060
    // 访问 http://localhost:6060/debug/pprof/
    
    // 你的 bot 代码...
}
```

### 2. 自定义 pprof 端口

如果需要自定义端口，可以手动启动：

```go
package main

import (
    "net/http"
    _ "net/http/pprof"
    
    "github.com/KomeiDiSanXian/remilia"
    "github.com/sirupsen/logrus"
)

func main() {
    // 启动 pprof 服务在自定义端口
    go func() {
        logrus.Info("pprof server starting on :8888")
        if err := http.ListenAndServe(":8888", nil); err != nil {
            logrus.Errorf("pprof server error: %v", err)
        }
    }()
    
    engine := remilia.NewEngine()
    // 你的 bot 代码...
}
```

---

## 📊 CPU 性能分析

### 1. 采集 CPU Profile

#### 方法 A: 使用浏览器

访问：`http://localhost:6060/debug/pprof/profile?seconds=30`

浏览器会下载一个 profile 文件。

#### 方法 B: 使用命令行

```bash
# 采集 30 秒的 CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# 等待 30 秒后，进入交互式分析界面
(pprof) top 10    # 显示 CPU 占用前 10 的函数
(pprof) list FunctionName  # 查看具体函数的代码
(pprof) web       # 生成可视化图表（需要安装 graphviz）
```

### 2. 分析 CPU Profile

```bash
# 查看最耗 CPU 的函数
(pprof) top 20

# 示例输出
Showing nodes accounting for 1250ms, 83.33% of 1500ms total
      flat  flat%   sum%        cum   cum%
     300ms 20.00% 20.00%      500ms 33.33%  github.com/KomeiDiSanXian/remilia.(*Engine).ProcessEvent
     250ms 16.67% 36.67%      400ms 26.67%  github.com/KomeiDiSanXian/remilia.(*Matcher).Match
     200ms 13.33% 50.00%      200ms 13.33%  regexp.(*Regexp).MatchString
     150ms 10.00% 60.00%      150ms 10.00%  encoding/json.Unmarshal
```

**解读**：
- `flat`: 函数本身消耗的 CPU 时间
- `cum`: 函数及其调用的所有函数消耗的 CPU 时间
- 关注 `cum` 值大的函数，可能是优化入口

### 3. 生成火焰图

```bash
# 采集 profile
go tool pprof -raw -output=cpu.pprof http://localhost:6060/debug/pprof/profile?seconds=30

# 使用 go-torch 生成火焰图（需要安装）
go-torch -b cpu.pprof
```

---

## 💾 内存分析

### 1. 采集 Heap Profile

```bash
# 分析堆内存使用
go tool pprof http://localhost:6060/debug/pprof/heap

# 进入交互界面
(pprof) top 10        # 内存占用前 10
(pprof) list FunctionName  # 查看具体分配位置
```

### 2. 查看内存分配统计

```bash
# 查看内存分配次数（alloc_objects）
go tool pprof -alloc_objects http://localhost:6060/debug/pprof/heap

# 查看内存分配大小（alloc_space）
go tool pprof -alloc_space http://localhost:6060/debug/pprof/heap

# 查看当前内存使用（inuse_space）
go tool pprof -inuse_space http://localhost:6060/debug/pprof/heap
```

### 3. 识别内存泄漏

```bash
# 在不同时间点采集两次 heap profile
go tool pprof -base heap1.pprof heap2.pprof

# 对比差异，找出增长的内存
(pprof) top
(pprof) list FunctionName
```

### 4. 查看对象池效率

Remilia 提供了对象池统计：

```go
import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/sirupsen/logrus"
)

// 定期输出对象池统计
func monitorPoolStats() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        stats := remilia.ContextPoolStats()
        logrus.WithFields(logrus.Fields{
            "gets":     stats.Gets,
            "puts":     stats.Puts,
            "news":     stats.News,
            "hit_rate": stats.HitRate,
        }).Info("Context pool stats")
    }
}
```

---

## 🧵 Goroutine 分析

### 1. 查看当前 Goroutine

访问：`http://localhost:6060/debug/pprof/goroutine?debug=2`

输出所有 goroutine 的堆栈信息。

### 2. 检测 Goroutine 泄漏

```bash
# 在不同时间采集两次 goroutine profile
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutine1.txt
# 等待一段时间
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutine2.txt

# 对比两次的 goroutine 数量
# 如果持续增长，可能存在泄漏
```

### 3. 分析 Goroutine 堆栈

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine

(pprof) top 10   # 查看创建最多 goroutine 的位置
(pprof) traces   # 查看所有 goroutine 的调用栈
```

---

## 🔒 锁竞争分析

### 1. 启用 Mutex Profile

在代码中启用 mutex profiling：

```go
import "runtime"

func init() {
    // 启用 mutex profiling
    runtime.SetMutexProfileFraction(1)
    // 设置为 1 表示记录所有的锁竞争
    // 生产环境建议设置为较小的值，如 1000
}
```

### 2. 分析锁竞争

```bash
go tool pprof http://localhost:6060/debug/pprof/mutex

(pprof) top 10   # 查看锁竞争最严重的位置
(pprof) list FunctionName  # 查看具体代码
```

### 3. 优化建议

- 减少锁的持有时间
- 使用读写锁（RWMutex）替代互斥锁
- 考虑使用 sync.Map 或分片锁
- 减小临界区的范围

---

## 🔍 阻塞分析

### 1. 启用 Block Profile

```go
import "runtime"

func init() {
    // 启用 block profiling
    runtime.SetBlockProfileRate(1)
}
```

### 2. 分析阻塞情况

```bash
go tool pprof http://localhost:6060/debug/pprof/block

(pprof) top 10   # 查看阻塞时间最长的操作
```

---

## 📈 实时监控

### 1. 使用 Prometheus + Grafana

Remilia 提供 Prometheus 指标：

```go
import (
    "github.com/KomeiDiSanXian/remilia"
    "github.com/KomeiDiSanXian/remilia/middleware"
)

func main() {
    engine := remilia.NewEngine()
    
    // 启用 Metrics 中间件
    engine.Use(middleware.Metrics())
    
    // Prometheus 指标暴露在 http://localhost:6060/metrics
    // 你的 bot 代码...
}
```

### 2. Grafana 监控面板

导入预配置的 Grafana dashboard：

```json
{
  "panels": [
    {
      "title": "事件处理 QPS",
      "targets": [
        {
          "expr": "rate(remilia_events_total[1m])"
        }
      ]
    },
    {
      "title": "事件处理延迟",
      "targets": [
        {
          "expr": "histogram_quantile(0.95, remilia_handler_duration_seconds_bucket)"
        }
      ]
    },
    {
      "title": "错误率",
      "targets": [
        {
          "expr": "rate(remilia_errors_total[1m])"
        }
      ]
    }
  ]
}
```

---

## 🎯 性能优化案例

### 案例 1: 优化正则表达式匹配

**问题**: CPU profile 显示 `regexp.MatchString` 占用 20% CPU

**原因**: 每次匹配都重新编译正则表达式

**解决**:
```go
// ❌ 错误：每次都编译
func OnRegexBad(pattern string) Rule {
    return func(ctx *Context) bool {
        // 每次调用都会编译正则表达式！
        matched, _ := regexp.MatchString(pattern, ctx.GetMessageContent())
        return matched
    }
}

// ✅ 正确：预编译正则表达式
func OnRegex(pattern string) Rule {
    re := regexp.MustCompile(pattern) // 编译一次
    return func(ctx *Context) bool {
        return re.MatchString(ctx.GetMessageContent())
    }
}
```

**效果**: CPU 占用降低 80%

---

### 案例 2: 减少内存分配

**问题**: Heap profile 显示大量 Context 对象分配

**原因**: 没有使用对象池

**解决**:
```go
// ❌ 错误：每次都创建新对象
func ProcessEvent(event *dto.Payload) {
    ctx := &Context{
        event: event,
        state: make(State),
    }
    // 处理...
}

// ✅ 正确：使用对象池
func ProcessEvent(event *dto.Payload) {
    ctx := NewContext(event, api)  // 从对象池获取
    defer ctx.Release()             // 归还到对象池
    // 处理...
}
```

**效果**: 内存分配减少 77%，GC 压力降低

---

### 案例 3: 优化锁竞争

**问题**: Mutex profile 显示 `Engine.mu` 竞争严重

**原因**: 所有事件处理都需要获取全局锁

**解决**:
```go
// ❌ 错误：使用全局锁
type Engine struct {
    mu       sync.Mutex
    matchers []*Matcher
}

func (e *Engine) ProcessEvent(ctx *Context) {
    e.mu.Lock()
    matchers := e.matchers
    e.mu.Unlock()
    // 处理...
}

// ✅ 正确：使用读写锁 + 事件类型索引
type Engine struct {
    mu           sync.RWMutex
    matcherIndex map[dto.EventType][]*Matcher
}

func (e *Engine) ProcessEvent(ctx *Context) {
    e.mu.RLock()  // 只需要读锁
    matchers := e.matcherIndex[ctx.GetEventType()]
    e.mu.RUnlock()
    // 处理...
}
```

**效果**: 锁竞争减少 90%，吞吐量提升 3x

---

## 📚 最佳实践

### 1. 定期进行性能分析

- 在开发环境定期采集 profile
- 在压测环境进行性能基准测试
- 在生产环境启用采样（低频率）

### 2. 建立性能基准

```go
// 在 _test.go 文件中编写 benchmark
func BenchmarkProcessEvent(b *testing.B) {
    engine := NewEngine()
    ctx := NewContext(testEvent, testAPI)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.ProcessEvent(ctx)
    }
}

// 运行 benchmark
// go test -bench=. -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof
```

### 3. 监控关键指标

- 事件处理 QPS
- P95/P99 延迟
- 错误率
- Goroutine 数量
- 内存使用量

### 4. 优化优先级

1. 先优化算法和数据结构
2. 再优化资源使用（内存、CPU）
3. 最后考虑并发优化

### 5. 避免过早优化

> "Premature optimization is the root of all evil" - Donald Knuth

- 先测量，再优化
- 关注 20% 的热点代码
- 保持代码可读性

---

## 🛠️ 工具清单

### 必备工具

- `go tool pprof`: Go 内置性能分析工具
- `graphviz`: 生成可视化图表
  ```bash
  # macOS
  brew install graphviz
  
  # Ubuntu
  sudo apt-get install graphviz
  
  # Windows
  choco install graphviz
  ```

### 推荐工具

- `go-torch`: 生成火焰图
  ```bash
  go install github.com/uber/go-torch@latest
  ```

- `pprof`: Google pprof 工具
  ```bash
  go install github.com/google/pprof@latest
  ```

- `hey`: HTTP 压测工具
  ```bash
  go install github.com/rakyll/hey@latest
  ```

---

## 🔗 相关资源

- [Go pprof 官方文档](https://pkg.go.dev/net/http/pprof)
- [Go 性能优化指南](https://dave.cheney.net/high-performance-go-workshop/dotgo-paris.html)
- [Prometheus 监控最佳实践](https://prometheus.io/docs/practices/naming/)

---

## 💡 下一步

1. 阅读 [分布式追踪集成指南](TRACING_INTEGRATION.md)
2. 查看 [性能测试报告](PERFORMANCE.md)
3. 尝试 [benchmark 示例](../realistic_bench_test.go)

---

**提示**: 如有性能问题，请先采集 profile 数据，然后在 GitHub Issues 中提供详细信息。

