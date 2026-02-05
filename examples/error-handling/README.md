# Error Handling Example

完善的错误处理示例

## 特性

- ✅ 多种错误类型演示
- ✅ 自定义错误类型
- ✅ 错误包装和解包
- ✅ 重试机制
- ✅ 权限检查
- ✅ Panic恢复

## 快速开始

```bash
cp config.example.yaml config.yaml
go run main.go
```

## 命令

- `/success` - 成功场景
- `/error` - 一般错误
- `/panic` - Panic错误(会被恢复)
- `/invalid` - 无效输入错误
- `/notfound` - 资源不存在
- `/permission` - 权限错误
- `/retry` - 重试场景

## 错误处理最佳实践

1. **分类处理**: 根据错误类型采取不同策略
2. **日志记录**: 完整记录错误上下文
3. **用户友好**: 返回易于理解的错误消息
4. **错误包装**: 使用 `fmt.Errorf` 和 `%w`
5. **重试机制**: 对临时性错误进行重试
