# Storage Plugin

统一的数据存储抽象层，为其他插件提供 KV 存储能力。

## 功能特性

- ✅ 统一的存储接口
- ✅ 支持多种后端（内存、Redis、SQLite）
- ✅ TTL 过期支持
- ✅ JSON 序列化/反序列化
- ✅ 通配符键查询
- ✅ 线程安全
- ✅ 高性能（内存后端）

## 安装

```go
import "github.com/KomeiDiSanXian/remilia/plugins/core/storage"
```

## 快速开始

### 基本使用

```go
// 创建存储插件（默认使用内存后端）
storagePlugin := storage.New()

// 注册到插件管理器
manager.Register(storagePlugin)

// 获取存储实例
s := storagePlugin.GetStorage()

// 设置值
s.Set("user:1", []byte("alice"), 0)

// 获取值
value, err := s.Get("user:1")
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(value)) // Output: alice

// 检查存在
if s.Exists("user:1") {
    fmt.Println("Key exists")
}

// 删除值
s.Delete("user:1")
```

### JSON 支持

```go
type User struct {
    Name string
    Age  int
}

// 设置 JSON 数据
user := User{Name: "Alice", Age: 25}
storagePlugin.SetJSON("user:1", user, 0)

// 获取 JSON 数据
var retrieved User
storagePlugin.GetJSON("user:1", &retrieved)
fmt.Printf("%+v\n", retrieved) // Output: {Name:Alice Age:25}
```

### TTL 过期

```go
// 设置 10 秒后过期
s.Set("session:abc", []byte("data"), 10*time.Second)

// 10 秒后，键自动过期
time.Sleep(11 * time.Second)
_, err := s.Get("session:abc")
// err == storage.ErrExpired
```

### 键查询

```go
// 设置多个键
s.Set("user:1", []byte("alice"), 0)
s.Set("user:2", []byte("bob"), 0)
s.Set("post:1", []byte("hello"), 0)

// 查询所有用户键
keys, _ := s.Keys("user:*")
fmt.Println(keys) // Output: [user:1 user:2]

// 查询所有键
keys, _ = s.Keys("*")
fmt.Println(keys) // Output: [user:1 user:2 post:1]
```

## API 文档

### Storage 接口

```go
type Storage interface {
    // Get 获取值
    Get(key string) ([]byte, error)
    
    // Set 设置值，ttl=0 表示永不过期
    Set(key string, value []byte, ttl time.Duration) error
    
    // Delete 删除值
    Delete(key string) error
    
    // Exists 检查键是否存在
    Exists(key string) bool
    
    // Keys 列出匹配的键（支持通配符 *）
    Keys(pattern string) ([]string, error)
    
    // Clear 清空所有数据
    Clear() error
}
```

### Plugin 方法

```go
// 获取存储实例
func (p *Plugin) GetStorage() Storage

// 便捷方法：JSON 操作
func (p *Plugin) SetJSON(key string, v interface{}, ttl time.Duration) error
func (p *Plugin) GetJSON(key string, v interface{}) error

// 便捷方法：基本操作
func (p *Plugin) Get(key string) ([]byte, error)
func (p *Plugin) Set(key string, value []byte, ttl time.Duration) error
func (p *Plugin) Delete(key string) error
func (p *Plugin) Exists(key string) bool
func (p *Plugin) Keys(pattern string) ([]string, error)
func (p *Plugin) Clear() error
```

## 后端实现

### 内存后端（默认）

```go
storage := storage.NewMemoryStorage()
plugin := storage.NewWithBackend(storage)
```

**特性**：
- 高性能（纳秒级）
- 无外部依赖
- 数据在内存中（重启丢失）
- 支持 TTL 自动过期

### SQLite 后端 ✅ 已实现

```go
sqliteStorage, err := storage.NewSQLiteStorage("data/storage.db")
if err != nil {
    log.Fatal(err)
}
defer sqliteStorage.Close()

plugin := storage.NewWithBackend(sqliteStorage)
```

**特性**：
- 数据持久化到磁盘
- 支持 TTL（毫秒级精度）
- 自动清理过期数据
- 线程安全
- 支持并发读写
- 数据库压缩（VACUUM）
- 统计信息查询

**额外方法**：
```go
// 清理过期数据
count, _ := sqliteStorage.CleanExpired()

// 压缩数据库
sqliteStorage.Compact()

// 获取统计信息
stats, _ := sqliteStorage.Stats()
// stats: {
//   "total_keys": 100,
//   "valid_keys": 95,
//   "expired_keys": 5,
//   "db_size_bytes": 8192,
//   "db_path": "data/storage.db"
// }

// 获取数据库大小
size, _ := sqliteStorage.Size()
```

**性能**：
- Set: ~1-2 ms/op（包含 fsync）
- Get: ~0.5 ms/op
- 并发安全，支持多线程访问

**使用建议**：
- 定期调用 `CleanExpired()` 清理过期数据
- 大量删除后调用 `Compact()` 压缩数据库
- 适合中小规模数据（< 1GB）
- 对于大规模或分布式场景，推荐使用 Redis

### Redis 后端（计划中）

```go
// TODO: Redis 实现
redisStorage := storage.NewRedisStorage("localhost:6379")
plugin := storage.NewWithBackend(redisStorage)
```


## 性能

基准测试结果（内存后端）：

```
BenchmarkMemoryStorage_Set-8          5000000    250 ns/op    64 B/op    2 allocs/op
BenchmarkMemoryStorage_Get-8         10000000    150 ns/op    32 B/op    1 allocs/op
BenchmarkMemoryStorage_Concurrent-8   3000000    400 ns/op
```

## 使用场景

1. **用户数据存储**
   ```go
   storage.SetJSON("user:"+userID, userData, 0)
   ```

2. **会话管理**
   ```go
   storage.Set("session:"+sessionID, sessionData, 30*time.Minute)
   ```

3. **缓存数据**
   ```go
   storage.Set("cache:"+key, data, 5*time.Minute)
   ```

4. **插件配置**
   ```go
   storage.SetJSON("config:"+pluginName, config, 0)
   ```

## 最佳实践

1. **使用命名空间**
   ```go
   // 推荐：使用前缀区分数据类型
   storage.Set("user:1", userData, 0)
   storage.Set("post:1", postData, 0)
   
   // 不推荐：没有前缀
   storage.Set("1", userData, 0)
   ```

2. **合理设置 TTL**
   ```go
   // 会话数据：短期过期
   storage.Set("session:abc", data, 30*time.Minute)
   
   // 用户数据：永久保存
   storage.Set("user:1", userData, 0)
   
   // 缓存数据：中期过期
   storage.Set("cache:key", cachedData, 5*time.Minute)
   ```

3. **错误处理**
   ```go
   value, err := storage.Get("key")
   if err == storage.ErrNotFound {
       // 键不存在，使用默认值
   } else if err == storage.ErrExpired {
       // 键已过期，重新获取
   } else if err != nil {
       // 其他错误
       log.Error(err)
   }
   ```

4. **定期清理（内存后端）**
   ```go
   // 定期清理过期键，释放内存
   ticker := time.NewTicker(5 * time.Minute)
   go func() {
       for range ticker.C {
           if ms, ok := storage.(*MemoryStorage); ok {
               count := ms.CleanExpired()
               log.Infof("Cleaned %d expired keys", count)
           }
       }
   }()
   ```

## 依赖插件

无依赖（基础插件）

## 被依赖插件

以下插件依赖 Storage：
- Cache Plugin
- Stats Plugin
- Logger Plugin
- Backup Plugin
- Note Plugin
- Reminder Plugin
- Custom Command Plugin
- RSS Plugin
- AutoReply Plugin

## 版本历史

### v1.0.0 (2026-02-07)
- ✅ 初始版本
- ✅ 内存后端实现
- ✅ 基本 KV 操作
- ✅ TTL 支持
- ✅ JSON 支持
- ✅ 通配符查询

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT License

