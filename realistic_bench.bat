@echo off
echo ===================================
echo Remilia Framework 真实性能测试
echo 测试时间: %date% %time%
echo ===================================

echo.
echo 🧪 运行真实工作负载测试...
echo 说明: 包含复杂业务逻辑、内存压力、真实数据大小
go test -run=^$ -bench=BenchmarkRealisticEventProcessing -benchmem -count=3

echo.
echo 🌐 运行网络I/O模拟测试...
echo 说明: 模拟网络延迟1-5ms，测试包含API调用的场景
go test -run=^$ -bench=BenchmarkWithNetworkIO -benchmem -count=3

echo.
echo 💾 运行数据库操作模拟测试...
echo 说明: 模拟数据库查询延迟10-50ms
go test -run=^$ -bench=BenchmarkWithDatabaseSimulation -benchmem -count=3

echo.
echo 🧠 运行内存压力测试...
echo 说明: 512MB内存压力 + 大量临时对象分配
go test -run=^$ -bench=BenchmarkMemoryPressure -benchmem -count=3

echo.
echo ⚡ 运行真实并发测试...
echo 说明: 多种处理器 + 网络/数据库延迟 + 内存压力
go test -run=^$ -bench=BenchmarkConcurrentRealistic -benchmem -count=3

echo.
echo 💬 运行真实C2C消息混合测试...
echo 说明: 私聊指令+富媒体混合集合，验证单聊压测表现
go test -run=^$ -bench=BenchmarkC2CMessageMix -benchmem -count=3

echo.
echo 👥 运行群聊富媒体压测...
echo 说明: 群@消息+多媒体突发，验证群聊吞吐
go test -run=^$ -bench=BenchmarkGroupAttachmentBurst -benchmem -count=3

echo.
echo 📚 运行批量混合消息测试...
echo 说明: 混合群聊/单聊批量处理路径，测试ProcessEventBatch
go test -run=^$ -bench=BenchmarkBatchMixedTraffic -benchmem -count=3

echo.
echo 🚫 运行QQ API限流模拟测试...
echo 说明: 模拟20 QPS限制，测试真实API瓶颈
go test -run=^$ -bench=BenchmarkQQAPILimitSimulation -benchmem -count=3

echo.
echo 📦 运行大数据包处理测试...
echo 说明: 1KB-10KB消息，模拟图片/文件消息
go test -run=^$ -bench=BenchmarkLargePayload -benchmem -count=3

echo.
echo 📊 对比简单测试 vs 真实测试...
echo --- 简单测试（原有的）---
go test -run=^$ -bench=BenchmarkEngineProcessEvent -benchmem -count=1

echo --- 真实测试（新的）---
go test -run=^$ -bench=BenchmarkRealisticEventProcessing -benchmem -count=1

echo.
echo ===================================
echo 真实性能测试完成
echo ===================================
echo.
echo 📝 性能分析说明:
echo - 真实测试的QPS应该比简单测试低1-2个数量级
echo - 网络I/O测试的QPS会显著降低（受延迟限制）
echo - 内存压力测试展示GC对性能的影响
echo - API限流测试展示真实瓶颈（约20 QPS）
echo - 大数据包测试展示处理复杂消息的性能
echo.
echo 💡 预期结果:
echo - 真实工作负载: 10K-100K ops/sec（vs 原来的500万）
echo - 网络I/O场景: 1K-10K ops/sec
echo - 数据库场景: 100-1K ops/sec
echo - API限流场景: 约20 ops/sec
echo.
