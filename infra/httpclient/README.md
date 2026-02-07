# HTTP Client

一个功能丰富、易于使用的 Go HTTP 客户端库，提供链式调用、中间件支持、重试机制等特性。

## 特性

- ✅ **链式 API** - 流畅的链式调用风格
- ✅ **中间件支持** - 可扩展的中间件系统
- ✅ **重试机制** - 可配置的自动重试
- ✅ **超时控制** - 细粒度的超时设置
- ✅ **JSON 支持** - 内置 JSON 序列化/反序列化
- ✅ **表单支持** - 表单数据编码
- ✅ **查询参数** - 便捷的查询参数设置
- ✅ **日志记录** - 可插拔的日志接口
- ✅ **响应助手** - 丰富的响应处理方法
- ✅ **全局客户端** - 便捷的全局函数

## 安装

```bash
go get github.com/KomeiDiSanXian/remilia/httpclient
```

## 快速开始

### 基本使用

```go
import "github.com/KomeiDiSanXian/remilia/httpclient"

// GET 请求
resp, err := httpclient.Get("https://api.example.com/users").Do()
if err != nil {
    log.Fatal(err)
}
defer resp.Close()

body, _ := resp.String()
fmt.Println(body)

// POST JSON
data := map[string]interface{}{
    "name": "Alice",
    "age":  25,
}

resp, err = httpclient.Post("https://api.example.com/users").
    SetJSON(data).
    Do()
```

### 创建客户端

```go
client := httpclient.NewClient().
    SetBaseURL("https://api.example.com").
    SetTimeout(10 * time.Second).
    SetHeader("User-Agent", "MyApp/1.0")

// 使用客户端
resp, err := client.Get("/users/123").Do()
```

## 详细用法

### 1. HTTP 方法

支持所有标准 HTTP 方法：

```go
client := httpclient.NewClient()

// GET
client.Get("/users").Do()

// POST
client.Post("/users").Do()

// PUT
client.Put("/users/123").Do()

// DELETE
client.Delete("/users/123").Do()

// PATCH
client.Patch("/users/123").Do()

// HEAD
client.Head("/users").Do()

// OPTIONS
client.Options("/users").Do()
```

### 2. 设置请求头

```go
// 单个请求头
resp, err := client.Get("/users").
    SetHeader("Authorization", "Bearer token").
    Do()

// 批量设置
resp, err := client.Get("/users").
    SetHeaders(map[string]string{
        "Authorization": "Bearer token",
        "X-Request-ID":  "123",
    }).
    Do()

// 客户端级别的默认请求头
client.SetHeader("User-Agent", "MyApp/1.0")
```

### 3. 设置请求体

#### JSON 请求

```go
type User struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

user := User{Name: "Alice", Age: 25}

resp, err := client.Post("/users").
    SetJSON(user).
    Do()
```

#### 表单请求

```go
resp, err := client.Post("/login").
    SetForm(map[string]string{
        "username": "alice",
        "password": "secret",
    }).
    Do()
```

#### 自定义请求体

```go
body := strings.NewReader("custom data")

resp, err := client.Post("/data").
    SetBody(body).
    SetHeader("Content-Type", "text/plain").
    Do()
```

### 4. 查询参数

```go
// 单个参数
resp, err := client.Get("/users").
    SetQuery("page", "1").
    SetQuery("limit", "10").
    Do()

// 批量设置
resp, err := client.Get("/users").
    SetQueries(map[string]string{
        "page":  "1",
        "limit": "10",
        "sort":  "name",
    }).
    Do()

// 添加重复键
resp, err := client.Get("/users").
    AddQuery("tag", "go").
    AddQuery("tag", "http").
    Do()
```

### 5. 响应处理

```go
resp, err := client.Get("/users").Do()
if err != nil {
    log.Fatal(err)
}
defer resp.Close()

// 检查状态
if resp.IsSuccess() {
    fmt.Println("Success!")
}

if resp.IsError() {
    fmt.Println("Error!")
}

// 读取为字符串
body, err := resp.String()

// 读取为字节数组
bytes, err := resp.Bytes()

// 解析 JSON (使用 gjson)
jsonResult, err := resp.JSON()
name := jsonResult.Get("name").String()

// 反序列化到结构体
var user User
err = resp.Unmarshal(&user)
```

### 6. 便捷方法

```go
// 直接获取 JSON
result, err := client.Get("/users").DoJSON()
name := result.Get("name").String()

// 直接获取字符串
str, err := client.Get("/users").DoString()

// 直接获取字节数组
bytes, err := client.Get("/users").DoBytes()
```

### 7. 超时控制

```go
// 客户端级别
client := httpclient.NewClient().
    SetTimeout(10 * time.Second)

// 请求级别
resp, err := client.Get("/users").
    SetTimeout(5 * time.Second).
    Do()

// 使用自定义 Context
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

resp, err := client.Get("/users").
    SetContext(ctx).
    Do()
```

### 8. 中间件

#### 使用内置中间件

```go
client := httpclient.NewClient().
    Use(httpclient.AuthBearerMiddleware("your-token")).
    Use(httpclient.UserAgentMiddleware("MyApp/1.0")).
    Use(httpclient.TimeoutMiddleware(10 * time.Second))

// 为单个请求添加中间件
resp, err := client.Get("/users").
    Use(httpclient.AuthBearerMiddleware("special-token")).
    Do()
```

#### 创建自定义中间件

```go
func CustomMiddleware() httpclient.Middleware {
    return func(r *httpclient.Request) error {
        // 在请求前执行
        r.SetHeader("X-Custom-Header", "value")
        return nil
    }
}

client.Use(CustomMiddleware())
```

#### 内置中间件

- `AuthBearerMiddleware(token)` - Bearer Token 认证
- `AuthBasicMiddleware(user, pass)` - Basic 认证
- `UserAgentMiddleware(ua)` - 设置 User-Agent
- `TimeoutMiddleware(duration)` - 设置超时
- `HeaderMiddleware(headers)` - 批量设置请求头
- `LoggingMiddleware(logger)` - 日志记录
- `NewRateLimitMiddleware(interval)` - 速率限制

### 9. 重试机制

```go
retryConfig := &httpclient.RetryConfig{
    MaxRetries:     3,
    RetryWaitTime:  1 * time.Second,
    RetryMaxWait:   5 * time.Second,
    RetryCondition: httpclient.DefaultRetryCondition,
}

client := httpclient.NewClient().
    SetRetry(retryConfig)

// 请求失败时会自动重试
resp, err := client.Get("/api/data").Do()
```

### 10. 日志记录

```go
type MyLogger struct{}

func (l *MyLogger) Debugf(format string, args ...interface{}) {
    log.Printf("[DEBUG] "+format, args...)
}

func (l *MyLogger) Infof(format string, args ...interface{}) {
    log.Printf("[INFO] "+format, args...)
}

func (l *MyLogger) Warnf(format string, args ...interface{}) {
    log.Printf("[WARN] "+format, args...)
}

func (l *MyLogger) Errorf(format string, args ...interface{}) {
    log.Printf("[ERROR] "+format, args...)
}

client := httpclient.NewClient().
    SetLogger(&MyLogger{})
```

## 完整示例

```go
package main

import (
    "fmt"
    "log"
    "time"
    
    "github.com/KomeiDiSanXian/remilia/httpclient"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
    Age  int    `json:"age"`
}

func main() {
    // 创建客户端
    client := httpclient.NewClient().
        SetBaseURL("https://api.example.com").
        SetTimeout(10 * time.Second).
        Use(httpclient.AuthBearerMiddleware("your-token")).
        Use(httpclient.UserAgentMiddleware("MyApp/1.0"))
    
    // 设置重试
    client.SetRetry(&httpclient.RetryConfig{
        MaxRetries:     3,
        RetryWaitTime:  1 * time.Second,
        RetryMaxWait:   5 * time.Second,
        RetryCondition: httpclient.DefaultRetryCondition,
    })
    
    // GET 请求
    result, err := client.Get("/users").
        SetQuery("page", "1").
        SetQuery("limit", "10").
        DoJSON()
    
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println("Users:", result.Get("users").String())
    
    // POST 请求
    newUser := User{Name: "Alice", Age: 25}
    
    resp, err := client.Post("/users").
        SetJSON(newUser).
        Do()
    
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Close()
    
    if resp.IsSuccess() {
        var created User
        resp.Unmarshal(&created)
        fmt.Printf("Created user: %+v\n", created)
    }
    
    // PUT 请求
    updated := User{ID: 1, Name: "Alice", Age: 26}
    
    resp, err = client.Put("/users/1").
        SetJSON(updated).
        Do()
    
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Close()
    
    // DELETE 请求
    resp, err = client.Delete("/users/1").Do()
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Close()
    
    fmt.Println("User deleted:", resp.IsSuccess())
}
```

## API 参考

### Client 方法

- `NewClient()` - 创建新客户端
- `SetBaseURL(url)` - 设置基础 URL
- `SetTimeout(duration)` - 设置默认超时
- `SetHeader(key, value)` - 设置默认请求头
- `SetHeaders(headers)` - 批量设置请求头
- `SetHTTPClient(client)` - 设置底层 http.Client
- `SetLogger(logger)` - 设置日志记录器
- `SetRetry(config)` - 设置重试配置
- `Use(middleware)` - 添加中间件
- `Get(url)` - 创建 GET 请求
- `Post(url)` - 创建 POST 请求
- `Put(url)` - 创建 PUT 请求
- `Delete(url)` - 创建 DELETE 请求
- `Patch(url)` - 创建 PATCH 请求
- `Head(url)` - 创建 HEAD 请求
- `Options(url)` - 创建 OPTIONS 请求

### Request 方法

- `SetHeader(key, value)` - 设置请求头
- `SetHeaders(headers)` - 批量设置请求头
- `SetBody(body)` - 设置请求体
- `SetJSON(data)` - 设置 JSON 请求体
- `SetForm(data)` - 设置表单数据
- `SetQuery(key, value)` - 设置查询参数
- `SetQueries(queries)` - 批量设置查询参数
- `AddQuery(key, value)` - 添加查询参数（允许重复）
- `SetTimeout(duration)` - 设置超时
- `SetContext(ctx)` - 设置上下文
- `Use(middleware)` - 添加中间件
- `Do()` - 执行请求
- `DoJSON()` - 执行并解析 JSON
- `DoString()` - 执行并返回字符串
- `DoBytes()` - 执行并返回字节数组

### Response 方法

- `Bytes()` - 读取为字节数组
- `String()` - 读取为字符串
- `JSON()` - 解析为 JSON (gjson.Result)
- `Unmarshal(v)` - 反序列化到对象
- `IsSuccess()` - 检查是否成功 (2xx)
- `IsError()` - 检查是否错误 (4xx/5xx)
- `Close()` - 关闭响应体

## 性能

- 支持连接池
- 支持 HTTP/2
- 零额外内存分配（在合理使用下）
- 高效的 JSON 解析（使用 gjson）

## 最佳实践

1. **复用客户端** - 不要为每个请求创建新客户端
2. **设置超时** - 总是设置合理的超时时间
3. **关闭响应** - 使用 `defer resp.Close()` 确保资源释放
4. **错误处理** - 检查所有错误返回值
5. **使用中间件** - 将通用逻辑放入中间件
6. **日志记录** - 在生产环境启用日志

## 与标准库对比

### 标准库

```go
req, _ := http.NewRequest("POST", url, bytes.NewReader(jsonData))
req.Header.Set("Content-Type", "application/json")
req.Header.Set("Authorization", "Bearer token")
client := &http.Client{Timeout: 10 * time.Second}
resp, _ := client.Do(req)
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)
```

### httpclient

```go
resp, _ := httpclient.Post(url).
    SetJSON(data).
    SetHeader("Authorization", "Bearer token").
    SetTimeout(10 * time.Second).
    Do()
defer resp.Close()
body, _ := resp.Bytes()
```

## License

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

