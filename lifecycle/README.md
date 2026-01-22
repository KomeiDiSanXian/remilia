# Remilia Lifecycle Package

生命周期管理包，提供组件的启动、停止和状态管理。

## 功能

### Component 接口

所有需要生命周期管理的组件都应该实现此接口：

```go
type Component interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

### Manager - 生命周期管理器

统一管理多个组件的生命周期：

```go
manager := lifecycle.NewManager()

// 注册组件
manager.Register(comp1)
manager.Register(comp2)
manager.Register(comp3)

// 启动所有组件（按注册顺序）
err := manager.Start(ctx)

// 停止所有组件（逆序）
err := manager.Stop(ctx)

// 查询状态
state := manager.State()
uptime := manager.Uptime()
count := manager.ComponentCount()
```

## 生命周期状态

```go
const (
    StateCreated  // 已创建
    StateStarting // 启动中
    StateRunning  // 运行中
    StateStopping // 停止中
    StateStopped  // 已停止
    StateFailed   // 失败
)
```

## 特性

### 1. 顺序启动

组件按注册顺序启动：

```go
manager.Register(database)  // 先启动
manager.Register(cache)     // 然后启动
manager.Register(server)    // 最后启动
```

### 2. 逆序停止

组件按注册的逆序停止：

```go
// 停止顺序：server -> cache -> database
manager.Stop(ctx)
```

### 3. 启动失败回滚

如果某个组件启动失败，会自动回滚已启动的组件：

```go
manager.Register(comp1)  // 启动成功
manager.Register(comp2)  // 启动失败
manager.Register(comp3)  // 不会启动

// comp1 会被自动停止（回滚）
```

### 4. 优雅停止

即使某个组件停止失败，也会继续停止其他组件：

```go
manager.Register(comp1)
manager.Register(comp2)  // 停止失败
manager.Register(comp3)

// comp1 和 comp3 仍会被停止
```

## 使用示例

### 基础用法

```go
import (
    "context"
    "github.com/KomeiDiSanXian/remilia/lifecycle"
)

// 实现 Component 接口
type MyService struct {
    name string
}

func (s *MyService) Name() string {
    return s.name
}

func (s *MyService) Start(ctx context.Context) error {
    log.Printf("%s starting...", s.name)
    // 初始化服务
    return nil
}

func (s *MyService) Stop(ctx context.Context) error {
    log.Printf("%s stopping...", s.name)
    // 清理资源
    return nil
}

// 使用
func main() {
    manager := lifecycle.NewManager()
    
    manager.Register(&MyService{name: "database"})
    manager.Register(&MyService{name: "cache"})
    manager.Register(&MyService{name: "server"})
    
    ctx := context.Background()
    
    // 启动
    if err := manager.Start(ctx); err != nil {
        log.Fatal(err)
    }
    
    // 运行...
    log.Printf("Uptime: %v", manager.Uptime())
    
    // 停止
    if err := manager.Stop(ctx); err != nil {
        log.Error(err)
    }
}
```

### 使用 SimpleComponent

如果不想实现完整接口，可以使用 SimpleComponent：

```go
comp := lifecycle.NewSimpleComponent("my-service",
    func(ctx context.Context) error {
        // 启动逻辑
        return nil
    },
    func(ctx context.Context) error {
        // 停止逻辑
        return nil
    },
)

manager.Register(comp)
```

### 带超时的启动/停止

```go
// 启动超时
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := manager.Start(ctx); err != nil {
    log.Fatal("Start timeout or failed:", err)
}

// 停止超时
ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := manager.Stop(ctx); err != nil {
    log.Error("Stop timeout or failed:", err)
}
```

## 错误处理

### StartError

组件启动失败时返回：

```go
var startErr *lifecycle.StartError
if errors.As(err, &startErr) {
    log.Printf("Component %s failed to start", startErr.Component)
}
```

### StopError

组件停止失败时返回：

```go
var stopErr *lifecycle.StopError
if errors.As(err, &stopErr) {
    log.Printf("Stop failed: %v", stopErr.Unwrap())
}
```

### ErrInvalidState

状态无效时返回：

```go
var stateErr lifecycle.ErrInvalidState
if errors.As(err, &stateErr) {
    log.Printf("Invalid state: current=%s, expected=%s", 
        stateErr.Current, stateErr.Expected)
}
```

## 最佳实践

1. **组件独立性**: 每个组件应该能够独立启动和停止
2. **依赖顺序**: 按依赖顺序注册组件（被依赖的先注册）
3. **超时控制**: 使用 context 控制启动/停止超时
4. **错误处理**: 妥善处理启动/停止错误
5. **资源清理**: 在 Stop 方法中确保所有资源被清理

## 与其他包集成

### 与 Engine 集成

```go
type EngineComponent struct {
    engine *Engine
}

func (c *EngineComponent) Name() string {
    return "engine"
}

func (c *EngineComponent) Start(ctx context.Context) error {
    // Engine 启动逻辑
    return nil
}

func (c *EngineComponent) Stop(ctx context.Context) error {
    return c.engine.Shutdown(ctx)
}
```

### 与 Bot 集成

```go
manager := lifecycle.NewManager()
manager.Register(&EngineComponent{engine: engine})
manager.Register(&AdapterComponent{adapter: adapter})

bot := &Bot{
    lifecycle: manager,
}
```
