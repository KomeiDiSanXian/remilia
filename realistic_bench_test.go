package remilia

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	mathRand "math/rand"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// 内存压力测试使用的大内存块
var memoryBallast = make([]byte, 256<<20) // 256MB 内存压力

// 模拟复杂的用户数据
type UserProfile struct {
	ID       uint64            `json:"id"`
	Name     string            `json:"name"`
	Level    int               `json:"level"`
	Coins    int64             `json:"coins"`
	Items    []string          `json:"items"`
	Friends  []uint64          `json:"friends"`
	Settings map[string]string `json:"settings"`
	Avatar   []byte            `json:"avatar"`
}

// 生成真实大小的随机事件数据
func generateRealisticEvent(size int) *dto.Payload {
	// 生成随机用户数据
	user := UserProfile{
		ID:       uint64(mathRand.Int63()),
		Name:     generateRandomString(20),
		Level:    mathRand.Intn(100),
		Coins:    mathRand.Int63n(1000000),
		Items:    make([]string, mathRand.Intn(10)),
		Friends:  make([]uint64, mathRand.Intn(50)),
		Settings: make(map[string]string),
		Avatar:   make([]byte, 1024), // 1KB头像数据
	}

	// 填充随机数据
	for i := range user.Items {
		user.Items[i] = generateRandomString(15)
	}
	for i := range user.Friends {
		user.Friends[i] = uint64(mathRand.Int63())
	}
	for i := 0; i < 10; i++ {
		user.Settings[generateRandomString(8)] = generateRandomString(20)
	}
	rand.Read(user.Avatar)

	// 生成消息内容
	content := generateRandomString(size)

	detailMap := map[string]interface{}{
		"content":    content,
		"author":     user,
		"timestamp":  time.Now().Unix(),
		"msg_id":     generateRandomString(32),
		"extra_data": generateRandomString(200),
	}

	detailJSON, _ := json.Marshal(detailMap)

	return &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[mathRand.Intn(len(charset))]
	}
	return string(b)
}

// 模拟网络延迟
func simulateNetworkDelay() {
	// 模拟 1-5ms 的网络延迟
	delay := time.Duration(mathRand.Intn(4)+1) * time.Millisecond
	time.Sleep(delay)
}

// 模拟数据库操作
func simulateDBOperation() {
	// 模拟 10-50ms 的数据库查询
	delay := time.Duration(mathRand.Intn(40)+10) * time.Millisecond
	time.Sleep(delay)
}

// 模拟复杂的业务逻辑
func simulateComplexLogic(data string) string {
	// 字符串处理
	processed := strings.ToUpper(data)
	processed = strings.ReplaceAll(processed, "TEST", "PROCESSED")

	// 简单的数学计算
	sum := 0.0
	for i, r := range processed {
		sum += math.Sin(float64(r)) * float64(i)
	}

	return fmt.Sprintf("%s_PROCESSED_%f", processed[:min(len(processed), 20)], sum)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BenchmarkRealisticEventProcessing 真实事件处理性能测试
func BenchmarkRealisticEventProcessing(b *testing.B) {
	// 确保内存压力
	_ = memoryBallast

	engine := NewEngine()
	var processedCount int64

	// 注册真实的处理器
	engine.OnAny(OnKeyword("hello")).Handle(func(ctx *Context) {
		content := ctx.GetMessageContent()
		result := simulateComplexLogic(content)
		ctx.Set("processed", result)
		atomic.AddInt64(&processedCount, 1)
	})

	engine.OnAny(OnPrefix("/cmd")).Handle(func(ctx *Context) {
		// 模拟命令处理
		parts := strings.Split(ctx.GetMessageContent(), " ")
		if len(parts) > 1 {
			ctx.Set("command", parts[0])
			ctx.Set("args", strings.Join(parts[1:], " "))
		}
		atomic.AddInt64(&processedCount, 1)
	})

	engine.OnC2C().Handle(func(ctx *Context) {
		// 模拟通用消息处理
		content := ctx.GetMessageContent()
		if len(content) > 100 {
			ctx.Set("long_message", true)
		}
		ctx.Set("processed_at", time.Now().Unix())
		atomic.AddInt64(&processedCount, 1)
	})

	// 生成多样化的测试数据
	events := make([]*dto.Payload, 1000)
	messages := []string{
		"hello world",
		"/cmd start game",
		"This is a very long message that contains more than 100 characters to test the performance with larger payloads and see how the system behaves",
		"简短消息",
		"/cmd help",
		"hello there",
		"random message",
	}

	for i := range events {
		content := messages[i%len(messages)]
		events[i] = generateRealisticEvent(len(content) * 2) // 更大的数据

		// 替换消息内容
		var detailMap map[string]interface{}
		json.Unmarshal(events[i].Detail, &detailMap)
		detailMap["content"] = content
		events[i].Detail, _ = json.Marshal(detailMap)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)

		// 模拟一些异步清理工作
		if i%100 == 0 {
			runtime.GC() // 触发GC来模拟真实环境
		}
	}

	b.ReportMetric(float64(atomic.LoadInt64(&processedCount)), "handlers_executed")
}

// BenchmarkWithNetworkIO 包含网络I/O的性能测试
func BenchmarkWithNetworkIO(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var apiCalls int64

	engine.OnC2C().Handle(func(ctx *Context) {
		content := ctx.GetMessageContent()

		// 模拟API调用
		simulateNetworkDelay()
		atomic.AddInt64(&apiCalls, 1)

		// 模拟响应处理
		result := simulateComplexLogic(content)
		ctx.Set("api_result", result)
	})

	events := make([]*dto.Payload, 100)
	for i := range events {
		events[i] = generateRealisticEvent(200)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}

	b.ReportMetric(float64(atomic.LoadInt64(&apiCalls)), "api_calls")
}

// BenchmarkWithDatabaseSimulation 包含数据库操作的性能测试
func BenchmarkWithDatabaseSimulation(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var dbQueries int64

	engine.OnAny(OnKeyword("query")).Handle(func(ctx *Context) {
		// 模拟数据库查询
		simulateDBOperation()
		atomic.AddInt64(&dbQueries, 1)

		// 模拟查询结果处理
		ctx.Set("db_result", map[string]interface{}{
			"user_id": mathRand.Int63(),
			"score":   mathRand.Intn(1000),
			"data":    generateRandomString(50),
		})
	})

	events := make([]*dto.Payload, 50)
	for i := range events {
		events[i] = generateRealisticEvent(100)

		// 确保包含关键词
		var detailMap map[string]interface{}
		json.Unmarshal(events[i].Detail, &detailMap)
		detailMap["content"] = "query user data"
		events[i].Detail, _ = json.Marshal(detailMap)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}

	b.ReportMetric(float64(atomic.LoadInt64(&dbQueries)), "db_queries")
}

// BenchmarkMemoryPressure 内存压力测试
func BenchmarkMemoryPressure(b *testing.B) {
	// 创建更大的内存压力
	ballast := make([]byte, 512<<20) // 512MB
	_ = ballast

	engine := NewEngine()

	// 注册内存密集型处理器
	engine.OnC2C().Handle(func(ctx *Context) {
		// 分配临时内存
		tempData := make([]byte, 1024) // 1KB临时数据
		rand.Read(tempData)

		// 进行内存密集型操作
		content := ctx.GetMessageContent()
		processed := make([]string, 0, 100)
		for i := 0; i < 50; i++ {
			processed = append(processed, fmt.Sprintf("%s_%d_%x", content, i, tempData[i%len(tempData)]))
		}

		ctx.Set("processed_data", processed)
	})

	// 生成大量不同的事件
	events := make([]*dto.Payload, 5000)
	for i := range events {
		events[i] = generateRealisticEvent(500 + mathRand.Intn(1000)) // 500-1500字节
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)

		// 定期触发GC来模拟真实压力
		if i%50 == 0 {
			runtime.GC()
		}
	}
}

// BenchmarkConcurrentRealistic 真实并发场景测试
func BenchmarkConcurrentRealistic(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var totalProcessed int64

	// 模拟多种处理器
	engine.OnAny(OnKeyword("buy")).Handle(func(ctx *Context) {
		// 模拟购买操作
		simulateNetworkDelay() // API调用
		simulateDBOperation()  // 数据库更新

		content := ctx.GetMessageContent()
		result := simulateComplexLogic(content)
		ctx.Set("purchase_result", result)
		atomic.AddInt64(&totalProcessed, 1)
	})

	engine.OnAny(OnPrefix("/admin")).Handle(func(ctx *Context) {
		// 模拟管理员操作
		simulateDBOperation()
		ctx.Set("admin_action", true)
		atomic.AddInt64(&totalProcessed, 1)
	})

	engine.OnC2C().Handle(func(ctx *Context) {
		// 模拟日志记录
		content := ctx.GetMessageContent()
		ctx.Set("logged", true)
		ctx.Set("content_length", len(content))
		atomic.AddInt64(&totalProcessed, 1)
	})

	// 生成多样化事件
	eventTemplates := []string{
		"buy item sword",
		"/admin kick user123",
		"hello everyone",
		"buy potion health",
		"normal message",
		"/admin ban spammer",
	}

	events := make([]*dto.Payload, 1000)
	for i := range events {
		template := eventTemplates[i%len(eventTemplates)]
		events[i] = generateRealisticEvent(len(template) * 3)

		// 设置消息内容
		var detailMap map[string]interface{}
		json.Unmarshal(events[i].Detail, &detailMap)
		detailMap["content"] = template
		events[i].Detail, _ = json.Marshal(detailMap)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := events[mathRand.Intn(len(events))]
			ctx := NewContext(event, nil)
			engine.ProcessEvent(ctx)
		}
	})

	b.ReportMetric(float64(atomic.LoadInt64(&totalProcessed)), "total_handlers")
}

// BenchmarkQQAPILimitSimulation 模拟QQ API限制
func BenchmarkQQAPILimitSimulation(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var apiCallsBlocked int64
	var apiCallsSuccess int64

	// 模拟20 QPS的API限制
	lastCall := time.Now()
	const minInterval = 50 * time.Millisecond // 20 QPS

	engine.OnAny(OnCommand("/send")).Handle(func(ctx *Context) {
		now := time.Now()
		if now.Sub(lastCall) < minInterval {
			// API限流
			atomic.AddInt64(&apiCallsBlocked, 1)
			ctx.Set("status", "rate_limited")
		} else {
			// 模拟成功的API调用
			simulateNetworkDelay()
			lastCall = now
			atomic.AddInt64(&apiCallsSuccess, 1)
			ctx.Set("status", "sent")
		}
	})

	engine.OnC2C().Handle(func(ctx *Context) {
		// 模拟消息处理但不调用API
		content := ctx.GetMessageContent()
		ctx.Set("processed", len(content) > 10)
	})

	// 生成大量API调用事件
	events := make([]*dto.Payload, 200)
	for i := range events {
		events[i] = generateRealisticEvent(100)

		var detailMap map[string]interface{}
		json.Unmarshal(events[i].Detail, &detailMap)
		if i%3 == 0 {
			detailMap["content"] = "/send hello world"
		} else {
			detailMap["content"] = "regular message"
		}
		events[i].Detail, _ = json.Marshal(detailMap)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)

		// 模拟连续请求
		if i%10 == 0 {
			time.Sleep(time.Microsecond * 100) // 微量延迟模拟真实场景
		}
	}

	b.ReportMetric(float64(atomic.LoadInt64(&apiCallsSuccess)), "api_success")
	b.ReportMetric(float64(atomic.LoadInt64(&apiCallsBlocked)), "api_blocked")
}

// BenchmarkLargePayload 大数据包处理��试
func BenchmarkLargePayload(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var largePayloadCount int64

	engine.OnC2C().Handle(func(ctx *Context) {
		content := ctx.GetMessageContent()

		// 处理大数据包
		if len(content) > 1000 {
			// 模拟复杂处理
			lines := strings.Split(content, "\n")
			processed := make([]string, 0, len(lines))
			for _, line := range lines {
				if len(line) > 10 {
					processed = append(processed, simulateComplexLogic(line))
				}
			}
			ctx.Set("processed_lines", processed)
			atomic.AddInt64(&largePayloadCount, 1)
		}
	})

	// 生成大数据包事件
	events := make([]*dto.Payload, 100)
	for i := range events {
		// 生成 1KB - 10KB 的消息
		size := 1024 + mathRand.Intn(9*1024)
		largeContent := generateRandomString(size)

		// 添加换行符模拟真实文本
		lines := make([]string, 0, size/50)
		for j := 0; j < len(largeContent); j += 50 {
			end := min(j+50, len(largeContent))
			lines = append(lines, largeContent[j:end])
		}
		content := strings.Join(lines, "\n")

		var detailMap = map[string]interface{}{
			"content": content,
			"author": map[string]interface{}{
				"id":   mathRand.Int63(),
				"name": generateRandomString(20),
			},
			"timestamp": time.Now().Unix(),
		}

		events[i] = &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: nil,
		}
		events[i].Detail, _ = json.Marshal(detailMap)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)

		// 模拟GC压力
		if i%20 == 0 {
			runtime.GC()
		}
	}

	b.ReportMetric(float64(atomic.LoadInt64(&largePayloadCount)), "large_payloads")
}

// BenchmarkC2CMessageMix 模拟真实C2C消息流量
func BenchmarkC2CMessageMix(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var (
		handlerHits        int64
		commandHits        int64
		attachmentsHandled int64
	)

	engine.OnAny(OnPrefix("/")).Handle(func(ctx *Context) {
		atomic.AddInt64(&commandHits, 1)
		ctx.Set("command", ctx.GetMessageContent())
	})

	engine.OnAny(OnKeyword("@bot")).Handle(func(ctx *Context) {
		ctx.Set("mention", true)
	})

	engine.OnC2C().Handle(func(ctx *Context) {
		var evt dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&evt); err == nil {
			atomic.AddInt64(&attachmentsHandled, int64(len(evt.Attachments)))
		}
		ctx.Set("length", len(ctx.GetMessageContent()))
		atomic.AddInt64(&handlerHits, 1)
	})

	templates := []struct {
		content     string
		attachments int
	}{
		{"hello from client", 0},
		{"/cmd start raid", 0},
		{"@bot 需要帮助", 0},
		{"/send image", 1},
		{"分享一张图片", 2},
		{"batch upload files", 3},
	}

	events := make([]*dto.Payload, 512)
	for i := range events {
		t := templates[i%len(templates)]
		events[i] = buildC2CPayload(t.content, randomAttachments(t.attachments))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(events[i%len(events)], nil)
		engine.ProcessEvent(ctx)
	}

	b.ReportMetric(float64(atomic.LoadInt64(&handlerHits)), "handlers")
	b.ReportMetric(float64(atomic.LoadInt64(&commandHits)), "commands")
	b.ReportMetric(float64(atomic.LoadInt64(&attachmentsHandled)), "attachments")
}

// BenchmarkGroupAttachmentBurst 模拟群聊高频艾特 + 富媒体
func BenchmarkGroupAttachmentBurst(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var (
		groupMentions int64
		mediaBytes    int64
	)

	engine.OnGroupAt().Handle(func(ctx *Context) {
		var evt dto.GroupAtMessageCreateEvent
		if err := ctx.DecodeEvent(&evt); err == nil {
			atomic.AddInt64(&groupMentions, 1)
			for _, att := range evt.Attachments {
				atomic.AddInt64(&mediaBytes, int64(att.Size))
			}
		}
		ctx.Set("group", true)
	})

	engine.OnAny(OnKeyword("report")).Handle(func(ctx *Context) {
		ctx.Set("report", time.Now().UnixNano())
	})

	groups := []string{"group_alpha", "group_beta", "group_gamma"}
	events := make([]*dto.Payload, 256)
	for i := range events {
		content := fmt.Sprintf("@bot 上传报告 %d", i)
		meta := map[string]any{
			"shard":  i % 4,
			"locale": "zh_CN",
		}
		groupsel := groups[i%len(groups)]
		attachments := randomAttachments(1 + mathRand.Intn(4))
		events[i] = buildGroupPayload(content, groupsel, attachments, meta)
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := events[mathRand.Intn(len(events))]
			ctx := NewContext(event, nil)
			engine.ProcessEvent(ctx)
		}
	})

	b.ReportMetric(float64(atomic.LoadInt64(&groupMentions)), "group_events")
	b.ReportMetric(float64(atomic.LoadInt64(&mediaBytes)), "media_bytes")
}

// BenchmarkBatchMixedTraffic 模拟批量混合消息
func BenchmarkBatchMixedTraffic(b *testing.B) {
	_ = memoryBallast

	engine := NewEngine()
	var (
		batchProcessed   int64
		attachmentsTotal int64
	)

	engine.OnC2C().Handle(func(ctx *Context) {
		var evt dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&evt); err == nil {
			atomic.AddInt64(&attachmentsTotal, int64(len(evt.Attachments)))
		}
	})

	engine.OnGroupAt().Handle(func(ctx *Context) {
		var evt dto.GroupAtMessageCreateEvent
		if err := ctx.DecodeEvent(&evt); err == nil {
			atomic.AddInt64(&attachmentsTotal, int64(len(evt.Attachments)))
		}
	})

	batch := buildMixedBatchFixtures(64)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessEventBatch(batch, nil)
		atomic.AddInt64(&batchProcessed, int64(len(batch)))
	}

	b.ReportMetric(float64(atomic.LoadInt64(&batchProcessed)), "events")
	b.ReportMetric(float64(atomic.LoadInt64(&attachmentsTotal)), "attachments")
}

func setupSimpleEngine() *Engine {
	engine := NewEngine()
	engine.OnAny(OnKeyword("hello")).Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})
	engine.OnAny(OnPrefix("/cmd")).Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})
	engine.OnC2C().Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})
	engine.OnAny(OnKeyword("query")).Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})
	return engine
}

type richMessageDetail struct {
	ID          string           `json:"id"`
	Content     string           `json:"content"`
	Timestamp   string           `json:"timestamp"`
	Author      dto.Author       `json:"author"`
	Attachments []dto.Attachment `json:"attachments,omitempty"`
	GroupOpenID string           `json:"group_openid,omitempty"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

func newRichMessageDetail(content string, attachments []dto.Attachment) richMessageDetail {
	return richMessageDetail{
		ID:        fmt.Sprintf("evt_%d", mathRand.Int63()),
		Content:   content,
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Author: dto.Author{
			UserOpenID: fmt.Sprintf("user_%06d", mathRand.Intn(1_000_000)),
		},
		Attachments: attachments,
		Metadata: map[string]any{
			"msg_id":    fmt.Sprintf("mid_%d", mathRand.Int63()),
			"source":    "loadgen",
			"client_ts": time.Now().UnixNano(),
		},
	}
}

func buildPayloadFromDetail(eventType dto.EventType, detail richMessageDetail) *dto.Payload {
	detailBytes, _ := json.Marshal(detail)
	return &dto.Payload{
		ID:        dto.EventID(detail.ID),
		Operation: dto.Dispatch,
		Detail:    detailBytes,
		Type:      eventType,
	}
}

func buildC2CPayload(content string, attachments []dto.Attachment) *dto.Payload {
	detail := newRichMessageDetail(content, attachments)
	return buildPayloadFromDetail(dto.C2CMessageCreate, detail)
}

func buildGroupPayload(content, groupID string, attachments []dto.Attachment, metadata map[string]any) *dto.Payload {
	detail := newRichMessageDetail(content, attachments)
	detail.GroupOpenID = groupID
	if len(metadata) > 0 {
		if detail.Metadata == nil {
			detail.Metadata = make(map[string]any)
		}
		for k, v := range metadata {
			detail.Metadata[k] = v
		}
	}
	return buildPayloadFromDetail(dto.GroupAtMessageCreate, detail)
}

func randomAttachments(count int) []dto.Attachment {
	if count <= 0 {
		return nil
	}
	types := []string{"image/jpeg", "image/png", "voice", "file", "video/mp4"}
	attachments := make([]dto.Attachment, count)
	for i := 0; i < count; i++ {
		attachments[i] = buildAttachment(types[mathRand.Intn(len(types))])
	}
	return attachments
}

func buildAttachment(contentType string) dto.Attachment {
	size := 32_768 + mathRand.Intn(8*1024*1024)
	width := 0
	height := 0
	switch contentType {
	case "image/jpeg", "image/png", "image/gif":
		width = 400 + mathRand.Intn(1600)
		height = 400 + mathRand.Intn(1600)
	case "video/mp4":
		width = 1280
		height = 720
		size = 5*1024*1024 + mathRand.Intn(30*1024*1024)
	case "voice":
		size = 256_000 + mathRand.Intn(2*1024*1024)
	}
	return dto.Attachment{
		Type:     contentType,
		FileName: fmt.Sprintf("%s_%d", strings.ReplaceAll(contentType, "/", "-"), mathRand.Intn(1_000_000)),
		Height:   height,
		Width:    width,
		Size:     size,
		URL:      fmt.Sprintf("https://cdn.example.com/%s/%d", contentType, mathRand.Intn(1_000_000)),
	}
}

func buildMixedBatchFixtures(batchSize int) []*dto.Payload {
	if batchSize <= 0 {
		return nil
	}
	events := make([]*dto.Payload, batchSize)
	groups := []string{"group_alpha", "group_beta", "group_gamma"}
	for i := 0; i < batchSize; i++ {
		if i%3 == 0 {
			metadata := map[string]any{"shard": i % 4, "priority": i % 2}
			events[i] = buildGroupPayload(
				fmt.Sprintf("@bot 批处理消息 %d", i),
				groups[i%len(groups)],
				randomAttachments(1+mathRand.Intn(3)),
				metadata,
			)
			continue
		}
		events[i] = buildC2CPayload(
			fmt.Sprintf("bulk c2c message %d", i),
			randomAttachments(i%2),
		)
	}
	return events
}
