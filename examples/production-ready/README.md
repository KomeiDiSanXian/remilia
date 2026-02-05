# Production Ready Example

生产环境最佳实践示例

## 特性

- ✅ 生产级别中间件配置
- ✅ 完善的错误处理
- ✅ 慢请求监控
- ✅ 健康检查
- ✅ 优雅关闭
- ✅ 性能监控

## 快速开始

```bash
cp config.example.yaml config.yaml
# 编辑配置文件
go run main.go
```

## 命令

- `/ping` - 健康检查
- `/status` - 系统状态
- `/help` - 帮助信息

## 生产环境配置要点

1. **日志级别**: 使用 `info` 或 `warn`
2. **日志格式**: 使用 `json` 便于日志分析
3. **中间件**: 使用 `ProductionSet()` 包含所有关键中间件
4. **监控**: 启用慢请求检测和健康检查
5. **关闭**: 实现优雅关闭机制
