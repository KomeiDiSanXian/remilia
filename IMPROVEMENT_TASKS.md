# Remilia 改进任务清单

**生成日期**: 2026-01-25  
**基于**: CODE_REVIEW_REPORT.md

---

## 🔥 紧急修复 (P0 - 本周完成)

### Bug #1: Config Watcher Timer 泄漏
- **文件**: `config/watcher.go:148-159`
- **任务**: 在 stop timer 后添加 channel drain
- **工时**: 2h
- **检查**:
  ```go
  if !debounceTimer.Stop() {
      select {
      case <-debounceTimer.C:
      default:
      }
  }
  ```

### Bug #2: Dedup Filter 竞态条件
- **文件**: `middleware/dedup.go:118-145`
- **任务**: 使用单锁保护 check-clean-add 流程
- **工时**: 4h
- **检查**: 使用 race detector 测试

### 改进 #1: Dedup LRU 缓存
- **文件**: `middleware/dedup.go`
- **任务**: 替换为 LRU 缓存实现
- **工时**: 3h
- **依赖**: `github.com/hashicorp/golang-lru/v2`

---

## ⚡ 高优先级 (P1 - 2周内完成)

### Bug #3: Webhook Adapter 启动竞态
- **文件**: `webhook_adapter.go:140-158`
- **任务**: 先启动 workers，再启动 HTTP server
- **工时**: 2h

### Bug #4: Lifecycle Rollback Context 泄漏
- **文件**: `lifecycle/lifecycle.go:203-220`
- **任务**: 所有 compCancel 使用 defer 调用
- **工时**: 1h

### Bug #5: Rate Limit 并发写入竞态
- **文件**: `middleware/middleware.go:360-380`
- **任务**: 简化锁逻辑，避免 RUnlock/Lock 间隙
- **工时**: 3h

### 改进 #2: 事件处理追踪
- **文件**: `core/context/context.go` (新增)
- **任务**: 
  - 添加 ExecutionTrace 结构
  - 实现 DumpTrace() 方法
  - 添加追踪中间件
- **工时**: 8h

### 改进 #3: 事件优先级队列
- **文件**: `core/engine/engine.go` (新增)
- **任务**:
  - 实现 PriorityEvent 和 EventQueue
  - 修改 ProcessEvent 使用优先级队列
  - 添加配置选项
- **工时**: 12h

### 改进 #4: Matcher 预编译
- **文件**: `core/engine/matcher.go`
- **任务**:
  - 实现 CompiledMatcher 结构
  - 添加 FastPath 检查
  - 按成本排序 rules
- **工时**: 8h

### 改进 #5: 批量事件处理
- **文件**: `middleware/batch.go` (新建)
- **任务**:
  - 实现 BatchProcessor
  - 添加批量处理中间件
  - 支持 flush 策略
- **工时**: 6h

### 改进 #6: 临时 Matcher 优化
- **文件**: `core/engine/temp_matcher.go`
- **任务**:
  - 添加水位线机制
  - 实现触发式清理
  - 优化清理算法
- **工时**: 4h

---

## 📋 中优先级 (P2 - 1个月内完成)

### Bug #6: Engine ProcessEvent 超时控制
- **文件**: `core/engine/engine.go`
- **任务**: 添加 ProcessEventWithTimeout 方法
- **工时**: 4h

### Bug #7: DLQ BlockUntilSpace 优化
- **文件**: `infra/dlq/dlq.go:136-150`
- **任务**: 提前检查 queueClosed 状态
- **工时**: 2h

### Bug #8: Token Manager Stop 保护
- **文件**: `openapi/auth/token/manager.go`
- **任务**: 使用 sync.Once 保护 Stop
- **工时**: 1h

### 改进 #7: Matcher 热重载
- **文件**: `core/engine/matcher.go`, `plugin/plugin.go`
- **任务**:
  - 实现 ReplaceHandler 方法
  - 添加 SmoothReload 机制
  - 保证零停机
- **工时**: 16h

### 改进 #8: Circuit Breaker 自动恢复
- **文件**: `middleware/circuitbreaker.go`
- **任务**:
  - 实现半开状态
  - 添加自动探测
  - 配置成功阈值
- **工时**: 6h

### 改进 #9: 智能降级策略
- **文件**: `middleware/degradation.go` (新建)
- **任务**:
  - 实现 DegradationManager
  - 定义降级规则
  - 添加自动触发
- **工时**: 12h

### 改进 #10: Webhook 签名验证
- **文件**: `adapter.go:152`, `webhook_adapter.go`
- **任务**:
  - 实现 HMAC 签名验证
  - 添加配置选项
  - 文档说明
- **工时**: 4h

### 改进 #11: 配置验证
- **文件**: `config/config.go`
- **任务**:
  - 添加 Validate 方法
  - 实现 schema 验证
  - 启动时检查
- **工时**: 4h

### 改进 #12: Matcher 统计信息
- **文件**: `core/engine/matcher.go`
- **任务**:
  - 添加调用次数
  - 记录平均延迟
  - 统计成功率
- **工时**: 3h

---

## 🎨 低优先级 (P3 - 持续改进)

### 改进 #13: OpenTelemetry 集成
- **任务**: 添加分布式追踪支持
- **工时**: 8h

### 改进 #14: 自适应并发控制
- **任务**: 实现动态并发度调整
- **工时**: 12h

### 改进 #15: 事件重播功能
- **任务**: 实现事件录制和回放
- **工时**: 6h

### 改进 #16: HTTP 管理 API
- **任务**: 添加运行时管理接口
- **工时**: 8h

### 改进 #17: 日志优化
- **任务**: 标准化结构化日志，添加采样
- **工时**: 4h

### 改进 #18: 文档完善
- **任务**: 补充 API 文档和示例
- **工时**: 16h

---

## 📊 Sprint 规划建议

### Sprint 1 (Week 1-2)
**主题**: 关键 Bug 修复和基础优化

**任务**:
- [ ] Bug #1: Config Watcher Timer 泄漏 (2h)
- [ ] Bug #2: Dedup Filter 竞态 (4h)
- [ ] Bug #3: Webhook Adapter 启动竞态 (2h)
- [ ] Bug #4: Lifecycle Rollback (1h)
- [ ] Bug #5: Rate Limit 竞态 (3h)
- [ ] 改进 #1: Dedup LRU 缓存 (3h)

**总工时**: 15h  
**团队**: 1 人全职或 2 人兼职

### Sprint 2 (Week 3-4)
**主题**: 可观测性和性能优化

**任务**:
- [ ] 改进 #2: 事件处理追踪 (8h)
- [ ] 改进 #3: 事件优先级队列 (12h)
- [ ] 改进 #4: Matcher 预编译 (8h)

**总工时**: 28h  
**团队**: 1-2 人全职

### Sprint 3 (Week 5-6)
**主题**: 高级特性实现

**任务**:
- [ ] 改进 #5: 批量事件处理 (6h)
- [ ] 改进 #6: 临时 Matcher 优化 (4h)
- [ ] Bug #6: Engine 超时控制 (4h)
- [ ] Bug #7-8: 其他 Bug 修复 (3h)
- [ ] 改进 #8: Circuit Breaker 自动恢复 (6h)
- [ ] 改进 #10: Webhook 签名验证 (4h)

**总工时**: 27h  
**团队**: 1-2 人全职

### Sprint 4 (Week 7-8)
**主题**: 热重载和降级策略

**任务**:
- [ ] 改进 #7: Matcher 热重载 (16h)
- [ ] 改进 #9: 智能降级策略 (12h)

**总工时**: 28h  
**团队**: 2 人全职

---

## ✅ 验收标准

### Bug 修复验收
- [ ] 通过 `go test -race` 测试
- [ ] 添加单元测试覆盖修复场景
- [ ] 压力测试验证（至少 10K events/sec，持续 1 小时）
- [ ] Code review 通过

### 功能改进验收
- [ ] 性能测试达到预期指标
- [ ] 文档更新完成
- [ ] 示例代码可运行
- [ ] 向后兼容性测试通过
- [ ] Code review 通过

### 性能指标
- [ ] 事件处理吞吐量 >= 40K events/sec
- [ ] P99 延迟 <= 20ms
- [ ] 内存使用 <= 150MB (10K matchers)
- [ ] CPU 使用 <= 25% (单核)
- [ ] 无 goroutine 泄漏
- [ ] 无内存泄漏

---

## 🧪 测试策略

### 单元测试
```bash
go test -v -race -count=1 ./...
```

### 性能测试
```bash
go test -bench=. -benchmem -benchtime=10s ./...
```

### 压力测试
```bash
# 使用 examples/benchmark
cd examples/benchmark
go run main.go -events=100000 -concurrency=100
```

### 竞态检测
```bash
go test -race -count=100 ./core/engine/...
go test -race -count=100 ./middleware/...
```

### 内存泄漏检测
```bash
go test -memprofile=mem.prof ./...
go tool pprof mem.prof
```

---

## 📈 度量指标

### 开发进度跟踪
- **完成的任务数** / 总任务数
- **修复的 Bug 数** / 总 Bug 数
- **代码覆盖率**: 目标 >= 85%
- **性能提升百分比**

### 质量指标
- **新增 Bug 数**: 目标 = 0
- **性能回退数**: 目标 = 0
- **代码审查通过率**: 目标 = 100%
- **测试通过率**: 目标 = 100%

### 性能指标
- **吞吐量提升**: 目标 4x
- **延迟降低**: 目标 2.5x
- **内存减少**: 目标 30%
- **CPU 减少**: 目标 33%

---

## 🔗 相关资源

### 参考文档
- [CODE_REVIEW_REPORT.md](./CODE_REVIEW_REPORT.md) - 完整的代码审查报告
- [docs/BEST_PRACTICES.md](./docs/BEST_PRACTICES.md) - 最佳实践指南
- [TESTING.md](./TESTING.md) - 测试指南

### 工具和库
- golang-lru: https://github.com/hashicorp/golang-lru
- OpenTelemetry: https://opentelemetry.io/docs/instrumentation/go/
- Prometheus: https://prometheus.io/docs/guides/go-application/

### 学习资源
- Go 并发模式: https://go.dev/blog/pipelines
- Copy-on-Write: https://en.wikipedia.org/wiki/Copy-on-write
- Circuit Breaker: https://martinfowler.com/bliki/CircuitBreaker.html

---

## 💬 反馈和讨论

如果你有任何问题或建议，请：
1. 提交 GitHub Issue
2. 在团队会议中讨论
3. 更新此文档

---

**最后更新**: 2026-01-25  
**维护者**: 开发团队  
**版本**: v1.0
