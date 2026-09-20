//go:build network

// 真机验证（webhook 路径）：GROUP_MEMBER_ADD 的 event_id 被动回复是否存在"登记滞后窗口"。
//
// 生产部署在 webhook 模式，且 WS 与 webhook 在 QQ 控制台只能二选一，因此本用例
// 不起 wsconn，而是直接跑与生产同款的 WebhookService（同一份验签、解析与事件流代码），
// 把 QQ 回调原样接进来，用真实事件量这条曲线。
//
// 待验证假设（2026-09-20 生产日志推断）：
//
//	同一 event_id 在事件刚到达时回复被 40034025 拒绝；晚 30 余秒再用**完全相同**的
//	id 发送却成功；同一时刻无效 id 仍被拒（说明平台校验在工作，只是该事件当时尚未
//	登记为"可回复"）。若假设成立，则失败原因与写法、内容、事件类型都无关，纯粹是
//	"回复太早"，越早越容易失败。
//
// 最小验证方式：等一条真实入群事件，收到后按预设偏移（默认 0/1/2/4 秒）用**同一个**
// event_id 依次回复，直到某次成功。首个成功的偏移即滞后上界，前一个偏移是下界；
// 若偏移 0 就成功，说明本次没有滞后窗口，40034025 属独立偶发而非时间相关。
//
// 运行（PowerShell，仓库根目录执行）：
//
//	go test -tags network -run TestQQWebhookEventIDWindowLive -v -timeout 30m ./platform/qq/
//
// 凭据与监听地址解析顺序（与其它 live 用例一致，凭据不写死在代码里）：
//   - QQ_APP_ID / QQ_APP_SECRET，未设置时回退 config.yaml 的 bot.qq.app_id / secret
//   - QQ_TEST_WEBHOOK_ADDR，未设置时回退 config.yaml 的 bot.qq.webhook.host/port
//     （默认 0.0.0.0:9000，与生产一致——把生产回调分流到本机该端口即可，验签使用
//     同一 AppSecret，转发必须原样保留 body 与 X-Signature-* 头）
//
// 可调环境变量：
//
//	QQ_WINDOW_PROBE_SCHEDULE  逗号分隔秒数（最多 5 个，默认 "0,1,2,4"）。平台规定同一
//	                          事件最多被动回复 5 次，生产实例若同时在回复同一事件会共同
//	                          消耗配额（超限为 40034128 而非 40034025）。
//	QQ_WINDOW_PROBE_EVENTS    收集几条事件后结束（默认 1）。
//	QQ_WINDOW_PROBE_TEXT      探针正文前缀（默认 "[remilia 时间窗验证]"）。
//	QQ_WINDOW_PROBE_SELFTEST  置 1 时只做工装自检：用 AppSecret 自签一条合成入群事件
//	                          发到本进程的 webhook 端点，验证验签/解析/事件流/取值链路后
//	                          立即结束，**不向任何群发送消息**。建议先跑一次自检。
//
// 注意：探针是**真的往群里发消息**，成功的那一次会留在群里（失败不产生消息）；撤回
// 时限只有 2 分钟（超时返回 40064004）。请用测试群，或在结果出来后立刻撤回。
package qq

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/config"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/protocol/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	windowProbeScheduleEnv = "QQ_WINDOW_PROBE_SCHEDULE"
	windowProbeEventsEnv   = "QQ_WINDOW_PROBE_EVENTS"
	windowProbeTextEnv     = "QQ_WINDOW_PROBE_TEXT"
	windowProbeSelfTestEnv = "QQ_WINDOW_PROBE_SELFTEST"

	// windowProbeWait 是单条入群事件的等待上限。
	windowProbeWait = 10 * time.Minute

	// windowProbeMaxSchedule 是探针偏移点上限：平台规定同一事件最多被动回复 5 次。
	windowProbeMaxSchedule = 5
)

// windowProbeEvent 是一条入群事件里与"何时回复"相关的原始信息。
type windowProbeEvent struct {
	eventID    string    // 事件最外层 id，即 event_id 的取值（形如 GROUP_MEMBER_ADD:<uuid>）
	groupID    string    // d.group_openid
	memberID   string    // d.member_openid
	generated  time.Time // d.timestamp：平台生成事件的时刻（被动回复窗口从此刻起算）
	receivedAt time.Time // 本进程从事件通道取出该事件的时刻
}

// windowProbeAttempt 是单次探针发送的结果。
type windowProbeAttempt struct {
	offset  time.Duration // 发出请求的时刻相对 receivedAt 的偏移
	latency time.Duration // 本次请求耗时（平台接受该 event_id 的时刻落在 [offset, offset+latency] 内）
	ok      bool
	code    int
}

// TestQQWebhookEventIDWindowLive 在 webhook 路径上量 event_id 被动回复的登记滞后窗口。
func TestQQWebhookEventIDWindowLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：" +
			"go test -tags network -run TestQQWebhookEventIDWindowLive -v -timeout 30m ./platform/qq/")
	}

	mgr := token.NewManager(&dto.BotInfo{AppID: appID, AppSecret: appSecret})
	t.Cleanup(mgr.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	require.NoError(t, mgr.WaitReadyWithContext(ctx),
		"获取 QQ access token 失败，请检查 QQ_APP_ID / QQ_APP_SECRET 是否有误")

	api := openapi.New(mgr)
	addr := webhookListenAddr(t)

	// 与生产同款：同一份 WebhookService（验签 + 解析 + 事件流），只是不回消息，
	// 只在收到入群事件后按预设偏移做探针发送。
	svc := NewWebhookServiceWithConfig(addr, &dto.BotInfo{
		AppID: appID, AppSecret: appSecret,
	}, config.WebhookConfig{})
	svc.WithAPI(api)
	require.NoError(t, svc.start(ctx),
		"启动 QQ Webhook 事件服务失败：请确认该地址未被占用，且 QQ 回调（或生产分流）能转发到它")
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = svc.stop(stopCtx)
	})

	stream := svc.EventStream()
	require.NotNil(t, stream, "start 后 EventStream 应立即可用")

	if isTruthy(os.Getenv(windowProbeSelfTestEnv)) {
		t.Logf("自检模式（%s=1）：只验证 webhook 工装，不向任何群发送消息", windowProbeSelfTestEnv)
		runWebhookSelfTest(t, addr, appSecret, stream)
		return
	}

	schedule := windowProbeSchedule(t)
	rounds := windowProbeRounds(t)
	text := windowProbeText(t)

	t.Logf("Webhook 监听 %s（验签 AppSecret 与生产一致）", addr)
	t.Logf("探针偏移 %v；计划收集 %d 条入群事件", schedule, rounds)
	t.Logf("每条事件的被动回复配额为 5 次，请确认生产实例不要同时对同一事件回复，否则可能撞 40034128")

	results := make([]windowProbeRound, 0, rounds)
	for i := range rounds {
		t.Logf("")
		t.Logf("================ 第 %d/%d 条事件：请触发一次入群 ================", i+1, rounds)
		t.Logf("用一个普通账号退出某个测试群、再重新加入（或让新成员入群）。")
		t.Logf("等待入群事件（最长 %v）……", windowProbeWait)
		t.Logf("===============================================================")

		ev := waitWindowProbeEvent(t, ctx, stream, windowProbeWait)
		attempts := probeEventIDWindow(t, ctx, api, ev, schedule, text)
		results = append(results, windowProbeRound{ev: ev, attempts: attempts})
		reportEventIDWindow(t, ev, attempts)
	}
	summarizeEventIDWindow(t, results)
}

// runWebhookSelfTest 用 AppSecret 自签一条合成的 GROUP_MEMBER_ADD，POST 到本进程的
// webhook 端点，验证"验签 → 解析 → 事件流 → 探针取值"整条 webhook 链路可用。
//
// 它校验的是工装本身（端点路径、方法、字段名、时间戳解析），不能替代"外部转发是否
// 保真"的验证——那必须用真实事件。但先跑它能避免把唯一的真机触发机会浪费在工装 bug 上。
func runWebhookSelfTest(t *testing.T, addr, appSecret string, stream <-chan *dto.Payload) {
	t.Helper()

	const (
		wantEventID = "GROUP_MEMBER_ADD:00000000-0000-0000-0000-000000000000"
		wantGroupID = "self-test-group-openid"
		wantMember  = "self-test-member-openid"
	)
	generated := time.Now()
	body := fmt.Sprintf(
		`{"op":0,"s":0,"t":"GROUP_MEMBER_ADD","id":%q,"d":{"timestamp":%d,"group_openid":%q,"member_openid":%q}}`,
		wantEventID, generated.Unix(), wantGroupID, wantMember)

	header := http.Header{}
	header.Set(webhook.HeaderTimestamp, strconv.FormatInt(time.Now().Unix(), 10))
	sign, err := webhook.NewWebhook(&dto.BotInfo{AppSecret: appSecret}).Sign(header, []byte(body))
	require.NoError(t, err, "自签失败：AppSecret 不可用")
	header.Set(webhook.HeaderSignature, hex.EncodeToString(sign))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	url := loopbackWebhookURL(addr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header = header
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "POST 到本进程 webhook 端点失败（%s）", url)
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "webhook 端点返回非 200：%s", respBody)
	t.Logf("自检：合成事件已投递 %s，响应 %s", url, respBody)

	ev := waitWindowProbeEvent(t, ctx, stream, 15*time.Second)
	assert.Equal(t, wantEventID, ev.eventID, "事件最外层 id 未被解析为 event_id")
	assert.Equal(t, wantGroupID, ev.groupID, "d.group_openid 未被解析")
	assert.Equal(t, wantMember, ev.memberID, "d.member_openid 未被解析")
	assert.WithinDuration(t, generated, ev.generated, time.Second, "d.timestamp 未被解析为事件生成时刻")
	t.Logf("自检通过：验签 → 解析 → 事件流 → 探针取值链路可用（本条不发送任何消息）")
}

// loopbackWebhookURL 把监听地址换成可从本机回连的 http URL。
// 监听 0.0.0.0/:: 时本机回连用 127.0.0.1。
func loopbackWebhookURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + strings.TrimSuffix(addr, "/") + "/webhook"
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/webhook"
}

// waitWindowProbeEvent 从事件流里取下一条 GROUP_MEMBER_ADD，丢弃其它事件。
func waitWindowProbeEvent(t *testing.T, ctx context.Context, stream <-chan *dto.Payload, wait time.Duration) windowProbeEvent {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("整体上下文结束，仍未等到 GROUP_MEMBER_ADD 事件：%v", ctx.Err())
		case <-timer.C:
			t.Fatalf("等待入群事件超时（%v）：请确认 QQ 回调已转发到本测试监听的地址", wait)
		case p := <-stream:
			if p == nil {
				t.Fatal("事件流已关闭，无法等待入群事件")
			}
			// receivedAt 取"从通道取出"的时刻：消费者始终空闲，缓冲不会堆积，
			// 因此它与 HTTP 处理结束的时刻相差在毫秒级。
			receivedAt := time.Now()
			ev, matched := asWindowProbeEvent(p, receivedAt)
			dto.ReleasePayload(p)
			if !matched {
				continue
			}
			t.Logf("收到入群事件 event_id=%s group_openid=%s member_openid=%s",
				ev.eventID, ev.groupID, ev.memberID)
			return ev
		}
	}
}

// asWindowProbeEvent 判断 payload 是否为入群事件并拷贝出探针所需字段。
// 返回的 struct 只含已拷贝的 string/time，不引用对象池内存，调用方随后可安全归还 payload。
func asWindowProbeEvent(p *dto.Payload, receivedAt time.Time) (windowProbeEvent, bool) {
	if p.Type != dto.GroupMemberAdd {
		return windowProbeEvent{}, false
	}
	ev := windowProbeEvent{
		eventID:    string(p.ID),
		groupID:    gjson.GetBytes(p.Detail, "group_openid").String(),
		memberID:   gjson.GetBytes(p.Detail, "member_openid").String(),
		receivedAt: receivedAt,
	}
	if ts := gjson.GetBytes(p.Detail, "timestamp").Int(); ts > 0 {
		ev.generated = time.Unix(ts, 0)
	}
	// 兜底：平台未给 group_openid 时用来源消息自己的 id 格式校验事件 id 是否成形。
	return ev, ev.eventID != "" && ev.groupID != ""
}

// probeEventIDWindow 按 schedule 的偏移用**同一个** event_id 依次尝试被动回复，
// 首次成功即停止（后续偏移不再消耗该事件的回复配额）。
func probeEventIDWindow(
	t *testing.T,
	ctx context.Context,
	api openapi.OpenAPI,
	ev windowProbeEvent,
	schedule []time.Duration,
	text string,
) []windowProbeAttempt {
	t.Helper()
	attempts := make([]windowProbeAttempt, 0, len(schedule))

	for i, offset := range schedule {
		if wait := offset - time.Since(ev.receivedAt); wait > 0 {
			select {
			case <-ctx.Done():
				t.Logf("上下文结束，停止探针：%v", ctx.Err())
				return attempts
			case <-time.After(wait):
			}
		}

		// 正文带上"到达后/生成后"两个相对时间：成功的那条会留在群里，
		// 可据此与日志、与事件自身的 timestamp 交叉核对。
		content := fmt.Sprintf("%s 第%d次 到达后%.2fs 生成后%.2fs", text, i+1,
			time.Since(ev.receivedAt).Seconds(), time.Since(ev.generated).Seconds())

		sentAt := time.Now()
		res, err := api.GroupChat(ctx, ev.groupID, &dto.Message{
			Type:       dto.TextMessage,
			Content:    content,
			EventID:    dto.EventID(ev.eventID),
			MessageSeq: uint64(i + 1),
		})
		at := windowProbeAttempt{offset: sentAt.Sub(ev.receivedAt), latency: time.Since(sentAt), ok: err == nil}

		if err != nil {
			at.code = qqErrorCode(err)
			t.Logf("偏移 %v：失败 code=%d %v", offset, at.code, err)
			if at.code != qqErrEventIDInvalid {
				t.Logf("非 40034025：40034128=该事件被动回复次数已被用尽（生产实例在同时回复？），" +
					"40034026=超过 5 分钟有效期，40034006=内容违规")
			}
		} else {
			t.Logf("偏移 %v：成功 id=%s", offset, res.Get("id").String())
		}
		attempts = append(attempts, at)

		if at.ok {
			return attempts
		}
	}
	return attempts
}

// reportEventIDWindow 逐条打印一次事件的探针结果。
//
// 每次尝试只提供半个约束，判定依据是"校验发生在请求内部"这一事实：
//   - 被 40034025 拒绝 → 该次校验时事件尚未登记为可回复 ⇒ 登记完成时刻 > 发出+耗时
//   - 发送成功        → 校验通过                          ⇒ 登记完成时刻 ≤ 发出+耗时
//
// 因此失败给出下界、成功给出上界，跨事件的合并见 summarizeEventIDWindow。
func reportEventIDWindow(t *testing.T, ev windowProbeEvent, attempts []windowProbeAttempt) {
	t.Helper()

	delivery := time.Duration(0)
	if !ev.generated.IsZero() {
		delivery = ev.receivedAt.Sub(ev.generated)
	}
	t.Logf("")
	t.Logf("---- event_id=%s ----", ev.eventID)
	if !ev.generated.IsZero() {
		t.Logf("平台生成于 %s，本进程收到于 %s，投递延迟 %v",
			ev.generated.Format("15:04:05.000"), ev.receivedAt.Format("15:04:05.000"), delivery.Round(time.Millisecond))
	} else {
		t.Logf("本进程收到于 %s（事件未带 timestamp）", ev.receivedAt.Format("15:04:05.000"))
	}

	// gen 把"收到后偏移"换算成"生成后偏移"，便于与生产日志（收到即回复）在同一刻线上对照。
	gen := func(d time.Duration) string {
		if ev.generated.IsZero() {
			return ""
		}
		return fmt.Sprintf("，生成后 %v", (delivery + d).Round(time.Millisecond))
	}

	for _, a := range attempts {
		done := a.offset + a.latency
		if a.ok {
			t.Logf("  发出于收到后 %-8v 成功   耗时 %-8v → 收到后 %v 已登记%s",
				a.offset.Round(time.Millisecond), a.latency.Round(time.Millisecond),
				done.Round(time.Millisecond), gen(done))
			break // 成功后不再尝试
		}
		t.Logf("  发出于收到后 %-8v 失败 %d 耗时 %-8v → 收到后 %v 仍未登记%s",
			a.offset.Round(time.Millisecond), a.code, a.latency.Round(time.Millisecond),
			done.Round(time.Millisecond), gen(done))
	}
	t.Logf("（注意：成功那条已留在群里，撤回时限 2 分钟）")
}

// windowProbeRound 是一轮探针的完整记录。
type windowProbeRound struct {
	ev       windowProbeEvent
	attempts []windowProbeAttempt
}

// summarizeEventIDWindow 合并全部事件的证据，给出平台完成登记的时刻区间。
//
// 取所有失败中的最晚"仍未登记"时刻作下界、所有成功中的最早"已登记"时刻作上界，
// 即得登记完成时刻所在区间；建议的首档重试间隔由该上界推出（见 suggestRetryDelay）。
func summarizeEventIDWindow(t *testing.T, rounds []windowProbeRound) {
	t.Helper()

	var (
		lower     time.Duration // 已确认"仍未登记"的最晚时刻
		upper     time.Duration // 已确认"已登记"的最早时刻上界
		haveLower bool
		haveUpper bool
		failCount int
	)
	for _, r := range rounds {
		for _, a := range r.attempts {
			done := a.offset + a.latency
			if a.ok {
				if !haveUpper || done < upper {
					upper, haveUpper = done, true
				}
				break
			}
			failCount++
			if !haveLower || done > lower {
				lower, haveLower = done, true
			}
		}
	}

	t.Logf("")
	t.Logf("==================== 汇总（%d 条事件）====================", len(rounds))
	switch {
	case !haveLower && !haveUpper:
		t.Logf("结论：没有任何完成发送的尝试（上下文提前结束）")
	case !haveLower:
		t.Logf("结论：每条事件的第一次尝试即成功（最迟收到后 %v 内完成）→ 本次未复现滞后窗口；"+
			"40034025 不能只归因于'回复太早'", upper.Round(time.Millisecond))
	case !haveUpper:
		t.Logf("结论：全部尝试点都被 40034025 拒绝（最晚收到后 %v 仍未登记）→ 登记上界 > %v，"+
			"请加大 %s 复测（最多 5 个点）",
			lower.Round(time.Millisecond), lower.Round(time.Millisecond), windowProbeScheduleEnv)
	default:
		t.Logf("结论：平台完成登记的时刻落在（收到事件后 %v，收到事件后 %v] 之间"+
			"（%d 次失败给出下界，成功给出上界）",
			lower.Round(time.Millisecond), upper.Round(time.Millisecond), failCount)
		t.Logf("      首档重试间隔建议 ≥ %v（%s 可覆盖；生产默认见 passive_eventid.go）",
			suggestRetryDelay(upper), passiveReplyDelaysEnv)
	}
	t.Logf("=========================================================")
}

// suggestRetryDelay 由实测上界给出建议的首档重试间隔：上界向上取整到 0.5s，再加一档余量。
func suggestRetryDelay(upper time.Duration) time.Duration {
	const step = 500 * time.Millisecond
	return (upper+step-1)/step*step + step
}

// windowProbeSchedule 解析 QQ_WINDOW_PROBE_SCHEDULE，未设置时用默认序列。
// 与被动回复重试共用同一套秒数解析（含"最多 N 个点"的平台配额校验）。
func windowProbeSchedule(t *testing.T) []time.Duration {
	t.Helper()
	delays, err := parseSecondsList(os.Getenv(windowProbeScheduleEnv), windowProbeMaxSchedule)
	require.NoError(t, err, "%s 解析失败", windowProbeScheduleEnv)
	if len(delays) == 0 {
		return []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second}
	}
	return delays
}

// windowProbeRounds 解析 QQ_WINDOW_PROBE_EVENTS，未设置或非法时按 1 条事件。
func windowProbeRounds(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(windowProbeEventsEnv))
	if raw == "" {
		return 1
	}
	n, err := strconv.Atoi(raw)
	require.NoError(t, err, "%s 必须是正整数", windowProbeEventsEnv)
	require.Positive(t, n, "%s 必须 > 0", windowProbeEventsEnv)
	return n
}

// windowProbeText 返回探针正文前缀。
func windowProbeText(t *testing.T) string {
	t.Helper()
	if text := strings.TrimSpace(os.Getenv(windowProbeTextEnv)); text != "" {
		return text
	}
	return "[remilia 时间窗验证]"
}
