# Remilia Errors Package (errutil)
通用错误处理工具包，提供错误包装、类型检查、堆栈追踪等功能。
## 快速参考
### 推荐 API
| 场景 | 推荐写法 |
|------|---------|
| 包级别哨兵错误 | var ErrFoo = errutil.New("foo failed") |
| 包装错误（带消息） | return errutil.Wrap(err, "operation failed") |
| 包装错误（带格式化） | return errutil.Wrapf(err, "plugin %s failed", name) |
| 带上下文字段的包装 | return errutil.WrapWithContext(err, "query failed", "table=users") |
| 动态错误（无哨兵） | return errutil.Newf("invalid value: %d", v) |
| 检查错误链 | errutil.Is(err, ErrFoo) |
### 不推荐
- 在核心包用裸 fmt.Errorf 创建非包装错误 -> 用 errutil.New 或 errutil.Newf
- 使用 %v 断开错误链 -> 必须用 %w 或 errutil.Wrapf
## 项目错误规范
1. 框架层公共哨兵错误定义在 errutil/errors.go
2. 包内特有错误使用 errutil.New 定义
3. 包装时始终保留错误链（%w）
4. 错误消息小写开头，不以句号结尾
详见 docs/02-user-guides/ERROR_HANDLING.md
