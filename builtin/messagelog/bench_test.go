package messagelog

// 基准套件。同机同参数据见 CHANGELOG v1.47.0：
//   - 四个固定基准（320 群场景）对比 v1.46.0 旧实现；
//   - 规模压测（320 → 1k → 3k → 10k 群）暴露热路径随群数扩展的瓶颈。
//
// 覆盖：
//   - 热缓存读：chat O(1) + 取 N 条 O(N)
//   - SQLite (chat_id, id DESC) 最近 N 查询（微秒级目标）
//   - 批量事务落盘吞吐（对照 flushLoop 参数 1000 条/批）
//   - 320 群并发入站热路径（eventToEntry + UUIDv7 + 入队 + 热缓存写入）
//   - 规模压测：生产缓存预算（per-chat 200 / 全局 50000）下热缓存查询与
//     并发记录随群数扩展
//
// 注意：SQLite 相关基准受磁盘/机器影响，对比时须同机同参。

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// benchGroupCount 模拟 320 个活跃群。
const benchGroupCount = 320

// BenchmarkHotCacheQueryGroup 热缓存读：320 群全部满 ring（容量 1000），并发读最近 50 条。
func BenchmarkHotCacheQueryGroup(b *testing.B) {
	l := New(1000)
	now := time.Now()
	for g := range benchGroupCount {
		gid := fmt.Sprintf("g%03d", g)
		for i := range 1000 {
			l.Record(RecordEntry{
				ChatID:    gid,
				UserID:    "u1",
				EventID:   fmt.Sprintf("%s-%d", gid, i),
				Content:   "消息",
				Timestamp: now.Add(time.Duration(i) * time.Second),
			})
		}
	}
	if msgs := l.QueryGroup("g001", 50); len(msgs) != 50 {
		b.Fatalf("setup: expected 50, got %d", len(msgs))
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			msgs := l.QueryGroup("g001", 50)
			if len(msgs) != 50 {
				return // 一致性已在 setup 校验，并行内不重复断言
			}
		}
	})
}

// BenchmarkSQLiteQueryRecent SQLite (chat_id, id DESC) 最近 50 条查询。
// 数据规模：约 10 万行（319 群 × 312 + 目标群 5000）。
// 并行读验证 WAL 多读者（OpenDB MaxOpenConns>1）收益。
func BenchmarkSQLiteQueryRecent(b *testing.B) {
	db, err := OpenDB(filepath.Join(b.TempDir(), "bench_recent.db"))
	if err != nil {
		b.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(b, db)

	now := time.Now()
	rows := make([]MessageRecord, 0, 100_000)
	for g := 1; g < benchGroupCount; g++ {
		gid := fmt.Sprintf("g%03d", g)
		for i := range 312 {
			rows = append(rows, MessageRecord{
				ChatID:    gid,
				UserID:    "u1",
				EventID:   fmt.Sprintf("%s-%d", gid, i),
				Content:   "历史",
				Timestamp: now.Add(time.Duration(i) * time.Second).UnixNano(),
				CreatedAt: now.UnixNano(),
			})
		}
	}
	for i := range 5000 {
		rows = append(rows, MessageRecord{
			ChatID:    "g000",
			UserID:    "u1",
			EventID:   fmt.Sprintf("g000-%d", i),
			Content:   "消息",
			Timestamp: now.Add(time.Duration(i) * time.Second).UnixNano(),
			CreatedAt: now.UnixNano(),
		})
	}
	if err := db.CreateInBatches(rows, 1000).Error; err != nil {
		b.Fatalf("seed: %v", err)
	}

	// 预热页缓存，排除首次查询 checkpoint 噪声
	var warm []MessageRecord
	if err := db.Where("chat_id = ?", "g000").Order("id DESC").Limit(50).Find(&warm).Error; err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var out []MessageRecord
		for pb.Next() {
			if err := db.Where("chat_id = ?", "g000").Order("id DESC").Limit(50).Find(&out).Error; err != nil {
				return
			}
		}
	})
}

// BenchmarkSQLiteWriteBatch 批量事务落盘吞吐（对照 flushLoop 参数：1000 条/批）。
func BenchmarkSQLiteWriteBatch(b *testing.B) {
	db, err := OpenDB(filepath.Join(b.TempDir(), "bench_write.db"))
	if err != nil {
		b.Fatalf("OpenDB: %v", err)
	}
	defer closeDB(b, db)

	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := make([]MessageRecord, 0, 1000)
		for j := range 1000 {
			rows = append(rows, MessageRecord{
				ChatID:    "g000",
				UserID:    "u1",
				EventID:   fmt.Sprintf("w-%d-%d", i, j),
				Content:   "消息",
				Timestamp: now.UnixNano(),
				CreatedAt: now.UnixNano(),
			})
		}
		tx := db.Begin()
		if tx.Error != nil {
			b.Fatalf("begin: %v", tx.Error)
		}
		if err := tx.CreateInBatches(rows, 500).Error; err != nil {
			tx.Rollback()
			b.Fatalf("insert: %v", err)
		}
		if err := tx.Commit().Error; err != nil {
			b.Fatalf("commit: %v", err)
		}
	}
}

// BenchmarkConcurrentRecordAsync 320 群并发入站热路径：eventToEntry + 入队 + ring 写入。
// 不绑定 DB（flushLoop 仅排空队列），隔离热路径吞吐；落盘吞吐见 BenchmarkSQLiteWriteBatch。
func BenchmarkConcurrentRecordAsync(b *testing.B) {
	l := New(1000)
	// 热路径基准隔离落盘与淘汰：per-chat 1000（同基准设定）、全局不限
	l.Start(Options{CachePerChat: 1000, CacheGlobal: -1})
	defer l.Stop()

	// 每个群一个独立事件/上下文；RunParallel worker 数 = GOMAXPROCS，
	// 每个 worker 固定命中一个群，模拟 320 群规模下的并发消息流
	evts := make([]platform.Event, benchGroupCount)
	ctxs := make([]*eventctx.Context, benchGroupCount)
	for g := range benchGroupCount {
		gid := fmt.Sprintf("g%05d", g)
		evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "基准消息",
			platform.WithSyntheticChat(platform.ChatInfo{ID: gid, IsGroup: true}))
		evts[g] = evt
		ctxs[g] = eventctx.NewContextFromEvent(evt, &platform.NoopSender{})
	}

	b.ReportAllocs()
	b.ResetTimer()
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		i := int(counter.Add(1)-1) % benchGroupCount
		evt := evts[i]
		ctx := ctxs[i]
		for pb.Next() {
			l.RecordAsync(evt, ctx)
		}
	})
}

// benchScaleScenarios 规模压测档位：320（当前部署量级）→ 1k → 3k → 10k 活跃群。
// 使用生产默认缓存预算（per-chat 200 / 全局 50000）；SQLite 查询按 chat_id 索引，
// 不随群数扩展，此处不重复测量。
var benchScaleScenarios = []int{320, 1000, 3000, 10000}

// benchPerChat / benchGlobal 生产默认热缓存预算。
const (
	benchPerChat = 200
	benchGlobal  = 50000
)

// seedCacheAtBudget 把缓存填到接近全局预算（目标群满环、其余群均分），
// 模拟生产稳态且 seeding 阶段不触发淘汰，避免拖慢 setup。
func seedCacheAtBudget(l *Logger, groups, perChat, global int) {
	now := time.Now()
	other := max(min((global-perChat)/max(1, groups-1), perChat), 1)
	for g := range groups {
		gid := fmt.Sprintf("g%05d", g)
		n := other
		if g == 0 {
			n = perChat
		}
		for i := range n {
			l.cache.add(gid, RecordEntry{
				ChatID:    gid,
				UserID:    "u1",
				EventID:   fmt.Sprintf("%s-%d", gid, i),
				Content:   "消息",
				Timestamp: now.Add(time.Duration(i) * time.Second),
			})
		}
	}
}

// BenchmarkHotCache_Scale 热缓存查询随群数扩展：查询路径 O(1) 定位会话 +
// O(n) ring 扫描，理论上不随群数增长；此基准验证预算淘汰/缓存结构
// 没有引入随群数扩展的开销。
func BenchmarkHotCache_Scale(b *testing.B) {
	for _, groups := range benchScaleScenarios {
		b.Run(fmt.Sprintf("%d_groups", groups), func(b *testing.B) {
			l := New(benchPerChat)
			seedCacheAtBudget(l, groups, benchPerChat, benchGlobal)
			if got := len(l.QueryGroup("g00000", 50)); got != 50 {
				b.Fatalf("setup: expected 50, got %d", got)
			}

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					msgs := l.QueryGroup("g00000", 50)
					if len(msgs) != 50 {
						return // 一致性已在 setup 校验，并行内不重复断言
					}
				}
			})
		})
	}
}

// BenchmarkRecordAsync_Scale 并发入站热路径随群数扩展：所有群活跃（每个事件
// 轮转命中不同会话）的最坏情形——全局预算满后持续触发淘汰。该基准曾暴露
// 近似 LRU 全表扫描随群数线性恶化（10k 群 33µs/op），cache 改为写序链表
// 定位 O(1) 后，退化收敛为 ring 重建抖动。
func BenchmarkRecordAsync_Scale(b *testing.B) {
	for _, groups := range benchScaleScenarios {
		b.Run(fmt.Sprintf("%d_groups", groups), func(b *testing.B) {
			l := New(benchPerChat)
			l.Start(Options{CachePerChat: benchPerChat, CacheGlobal: benchGlobal})
			defer l.Stop()
			seedCacheAtBudget(l, groups, benchPerChat, benchGlobal)

			evts := make([]platform.Event, groups)
			ctxs := make([]*eventctx.Context, groups)
			for g := range groups {
				gid := fmt.Sprintf("g%05d", g)
				evt := platform.NewSyntheticEvent(platform.EventKindGroupMessage, "基准消息",
					platform.WithSyntheticChat(platform.ChatInfo{ID: gid, IsGroup: true}))
				evts[g] = evt
				ctxs[g] = eventctx.NewContextFromEvent(evt, &platform.NoopSender{})
			}

			b.ReportAllocs()
			b.ResetTimer()
			var idx atomic.Int64
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					i := int(idx.Add(1)-1) % groups
					l.RecordAsync(evts[i], ctxs[i])
				}
			})
		})
	}
}
