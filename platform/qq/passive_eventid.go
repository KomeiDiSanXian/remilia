package qq

// passive_eventid.go — event_id 被动回复被平台短暂拒绝时的同值退避重试。
//
// event_id 是官方认可的一类被动消息：文档把它与"携带 msg_id 回复用户消息"并列，
// 列为「被动消息（响应事件）—— 对事件的回复」，取值"从事件最外层的 id 获取"。
// 群聊发送接口（/v2/groups/{group_openid}/messages）另外声明了支持的事件清单：
// INTERACTION_CREATE、GROUP_ADD_ROBOT、GROUP_MSG_RECEIVE（单聊为 INTERACTION_CREATE、
// C2C_MSG_RECEIVE、FRIEND_ADD）。
//
// 清单外的事件此前仍被平台接受：实测 2026-08-14 起入群欢迎以 GROUP_MEMBER_ADD
// 的 event_id 被动回复发送成功（当日提交即为此实现）；2026-09-18 18:26 起同一
// 代码路径开始返回 40034025"请求参数event_id无效"。
//
// 关键实测（2026-09-20，同一群、同一进程、同一 event_id 形式）：
// 22:11:05 与 22:26:06 的入群以 event_id 发送成功，22:13:18 / 22:14:31 的入群
// 以同样形式被拒。也就是说 40034025 在当前并非"该事件类型不支持"这类确定性
// 拒绝——类型不支持会稳定返回 40034027。
//
// 真机复验（2026-09-20 22:46，真实 GROUP_MEMBER_ADD event_id，官方 API）：
// 原样写法 "GROUP_MEMBER_ADD:<uuid>" 连续多次发送成功，而同一事件的**裸 id**
// （去掉 "GROUP_MEMBER_ADD:" 前缀）立即被拒 40034025。可见：
//
//   - 写法本身正确，"平台换了规范化写法"的猜测已证伪；
//   - 40034025 只在事件到达后的极短时间内出现（生产失败样本都是收到事件后 1~2 秒
//     立即回复，成功样本与本次复验都晚了一步），即平台把事件登记为"可回复"存在
//     短暂不一致。
//
// 决定性对照（同一 event_id、同一群、同一进程，2026-09-20）：
//
//	22:57:48  webhook 收到 GROUP_MEMBER_ADD:4a22cb91-… 后立刻回复（latency_ms=0）
//	          → 40034025 失败
//	22:58:22  用**完全相同**的 event_id 与写法再发一次 → 成功
//	           （同一时刻用无效 event_id 仍返回 40034025，说明校验正常工作，
//	            只是该事件当时尚未登记为"可回复"）
//
// webhook 路径实测（2026-09-20 23:27~23:37，本仓 webhook_eventid_window_live_test.go：
// 真实入群事件 + 真实分流回调 + 同一 AppSecret 验签，7 条事件共 19 次尝试）：
//
//	事件  生成 → 本进程收到   收到后立即回复             收到后 0.5s 回复
//	1     2.813s             成功（0.771s 内完成）       —
//	2     2.722s             40034025（0.193s 返回）     成功
//	3     2.747s             40034025（0.124s）          成功
//	4     2.120s             40034025（0.166s）          成功
//	5     4.025s             40034025 ×4（最晚 0.623s）  —（四次全败）
//	6     11.691s（事件 5 被重投）  40034025 ×4（最晚 0.607s）  —
//	7     2.605s             40034025 ×3（最晚 0.342s）  成功
//
// 分开看两类响应：被拒请求 120~218ms 就返回（只在请求早期做了一次校验），成功请求
// 480~771ms（真的投递了消息）。汇总全部样本：**"仍未登记"的最晚时刻是收到回调后
// 623ms，"已登记"的最早时刻是收到回调后 771ms**——窗口落在 (0.62s, 0.78s]，没有反例。
//
// 三条被真机数据否掉的解释：
//   - 不是"被拒即完成登记"：事件 7 连拒 3 次（+0 / +0.1 / +0.25）后第 4 次才成功；
//   - 不是以"事件生成时刻"计时：事件 5、6 在生成后 4.65s / 12.30s 的尝试仍被拒，
//     而事件 7 在生成后 3.71s 已成功；
//   - 不是内容或写法问题：该群欢迎内容只是纯文本 "123"，同样被拒；而探针正文成功。
//
// 结论：40034025 不是"该事件不支持回复"（那会稳定返回 40034027），而是**平台把本次
// 推送的事件登记为"可回复"的时刻，比它把回调交到我们手里晚约 0.6~0.8 秒**。窗口内
// 同一个 event_id 一律被拒，窗口一过立刻可用——"收到回调就回复"必然踩空。事件 5/6
// 还说明窗口按**每次投递**重新计时（同一条事件被重投时窗口会重来一遍）。
//
// 对策只有**同值退避重试**（每次尝试都记日志）：
//
//	原样 event_id → 等 1.5s 重试 → 等 2s 重试 → 等 3s 重试 → 仍失败则返回错误
//
// 首档取 1.5s：对实测上界 0.771s 留约 2 倍余量，代价只是被拒时欢迎语晚 1.5 秒出现。
// 重试次数必须克制：平台规定每个事件最多被动回复 5 次，初始 + 3 次重试用掉 4 次，
// 留 1 次余量。群聊被动回复有效期 5 分钟，本链总耗时约 6.5 秒，远在窗口内。
//
// 刻意**不**降级为主动消息：主动消息是另一种消息类型（不带 event_id 授权、需要群内
// 开启「允许主动在群聊内发言」、另计主动消息频次）。把插件作者写的"回复这个事件"
// 静默换成"机器人主动发言"，等于替他改了语义、改了权限要求，在没开主动发言的群里
// 只会换来一条必然失败的请求。平台既已认可 event_id 被动回复，被拒就应如实报错，
// 由调用方决定是否改用主动消息（自行发一条不带 token 的 SendRequest 即可）。
//
// 量出这条滞后曲线（生产 webhook 路径，无需额外基础设施）：每次重试都会记日志——
// 首次失败见核心出站日志的 [Outbound] message send failed（code=40034025），
// 之后每次重试失败记 DEBUG "[qq.Sender] event_id 被动回复重试仍未通过"（带 delay_ms），
// 首次成功记 WARN "[qq.Sender] event_id 被拒后重试成功"（带 delay_ms）。于是一次入群
// 就能还原成 [0s 失败, D1 失败, D2 成功]，滞后上界即 D2。
//
// 想提高分辨率时用 QQ_PASSIVE_REPLY_DELAYS 覆盖重试间隔（逗号分隔秒，最多 4 个），
// 例如 "0.25,0.5,1,2"：初始 + 4 次重试正好用满平台的 5 次回复上限。
//
// 只对 event_id 重试，不对 msg_id 重试：msg_id 被拒（40034005 / 40034024 /
// 40034031 / 40034128）意味着超出被动回复的时效或次数限制，重试没有意义。

import (
	stdctx "context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// QQ 发送接口中与 event_id 被动回复授权相关的错误码（官方错误码表）。
const (
	// qqErrEventIDInvalid 请求参数 event_id 无效——平台不再接受该事件的被动回复。
	qqErrEventIDInvalid = 40034025
	// qqErrEventIDExpired 请求参数 event_id 已过期（群聊 5 分钟 / 单聊 60 分钟）。
	qqErrEventIDExpired = 40034026
	// qqErrEventUnreplyable 该事件不支持回复消息。
	qqErrEventUnreplyable = 40034027
)

// qqErrCodeRe 从 openapi 错误字符串中提取错误码。
//
// 错误格式（见 openapi.doAndCheck）：
//
//	qq openapi: <url>: HTTP 400 code=40034025 message="请求参数event_id无效"
//	qq openapi: <url>: code=40034025 message="请求参数event_id无效"
//
// 响应体字段名可能是 code 或 err_code。
var qqErrCodeRe = regexp.MustCompile(`\b(?:code|err_code)=(\d+)`)

// qqErrorCode 返回错误中的 QQ 错误码，无法识别时返回 0。
func qqErrorCode(err error) int {
	if err == nil {
		return 0
	}
	m := qqErrCodeRe.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return 0
	}
	code, _ := strconv.Atoi(m[1])
	return code
}

// qqEventIDRejected 判断错误是否为"event_id 被动回复授权被平台拒绝"。
func qqEventIDRejected(err error) bool {
	switch qqErrorCode(err) {
	case qqErrEventIDInvalid, qqErrEventIDExpired, qqErrEventUnreplyable:
		return true
	default:
		return false
	}
}

// ────────────────────────────────────────────────────────────────────────────
// event_id 取值与重试序列
// ────────────────────────────────────────────────────────────────────────────

// passiveEventIDValue 返回请求携带的 event_id 值（与 buildDTOMessage 同优先级：
// Extra 手动注入 > ChatInfo.Tokens 自动填充）。
func passiveEventIDValue(req platform.SendRequest) string {
	if id := extractExtra(req.Message).EventID; id != "" {
		return id
	}
	return req.Target.Tokens[TokenEventID]
}

// defaultPassiveReplyDelays 是首次尝试（立即、原样 event_id）被拒后的同值重试间隔。
//
// 只重试同一个 event_id：真机复验（见文件头）已确认原样写法正确、裸 id 必被拒，
// 因此不存在"换写法"这一选项；40034025 属于平台登记短暂不一致，等一会儿再发同样的
// 值即可。最坏多花约 6.5 秒、多发三条被拒请求，只在被动回复被拒时发生。
//
// 首档取 1.5s 而非更短：webhook 路径实测（见文件头表格）"已登记"的最早上界是收到
// 事件后 0.771s，而"仍未登记"的最晚下界是 0.193s；1.5s 对该上界留约 2 倍余量，
// 代价仅是被拒后的欢迎语晚 1.5 秒出现。
//
// 不做"该事件类型不支持"的负向记忆：入群欢迎实测偶发成功，任何抑制都会降低本就
// 不高的成功率，而重试开销（每次入群最多三次被拒请求）可以忽略。
var defaultPassiveReplyDelays = []time.Duration{
	1500 * time.Millisecond,
	2 * time.Second,
	3 * time.Second,
}

// passiveReplyDelaysEnv 覆盖 event_id 被拒后的重试间隔（诊断/调优用）。
// 值为逗号分隔的秒数，最多 4 个（加上首次尝试正好用满平台"每事件最多回复 5 次"）。
// 命名沿用 config 里 QQ_TOKEN / QQ_SECRET 的约定。
const passiveReplyDelaysEnv = "QQ_PASSIVE_REPLY_DELAYS"

// parseSecondsList 解析逗号分隔的秒数列表，最多 max 个（空项被忽略）。
//
// 空值返回 (nil, nil)，表示不覆盖。非数字、负数、一个有效项都没有、或超过 max 个
// 时返回错误，由调用方决定如何告警与回退。
func parseSecondsList(raw string, max int) ([]time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	delays := make([]time.Duration, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		sec, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return nil, fmt.Errorf("%q 不是合法秒数：%w", p, err)
		}
		if sec < 0 {
			return nil, fmt.Errorf("%q 不能为负", p)
		}
		delays = append(delays, time.Duration(sec*float64(time.Second)))
	}
	if len(delays) == 0 {
		return nil, fmt.Errorf("未包含任何有效秒数")
	}
	if len(delays) > max {
		return nil, fmt.Errorf("最多 %d 个间隔（平台规定每个事件最多被动回复 5 次），当前 %d 个",
			max, len(delays))
	}
	return delays, nil
}

// parsePassiveReplyDelays 解析 QQ_PASSIVE_REPLY_DELAYS。
//
// 空值返回 (nil, nil)，表示不覆盖、使用默认间隔。最多 4 个：首次尝试 + 4 次重试
// 正好用满平台"每个事件最多被动回复 5 次"的配额。
func parsePassiveReplyDelays(raw string) ([]time.Duration, error) {
	delays, err := parseSecondsList(raw, 4)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", passiveReplyDelaysEnv, err)
	}
	return delays, nil
}

// envPassiveReplyDelays 读取环境变量覆盖，并原样返回该变量的值（供调用方记日志）。
//
// 未设置时返回 (nil, "", nil)；非法时返回 (nil, raw, err)，由调用方决定如何告警。
func envPassiveReplyDelays() (delays []time.Duration, raw string, err error) {
	raw = os.Getenv(passiveReplyDelaysEnv)
	delays, err = parsePassiveReplyDelays(raw)
	return delays, raw, err
}

// passiveReplyDelays 返回本次发送使用的重试间隔（测试可注入零延迟序列）。
func (s *qqSender) passiveReplyDelays() []time.Duration {
	if len(s.passiveRetryDelays) > 0 {
		return s.passiveRetryDelays
	}
	return defaultPassiveReplyDelays
}

// retryPassiveReply 在首次尝试被拒后按退避间隔重试同一个 event_id 被动回复。
//
// lastRes/lastErr 为首次尝试的结果，用于在重试无一成功（ctx 取消、间隔序列为空）
// 时原样返回。返回 nil 错误表示某次重试成功；否则返回最后一次尝试的错误。
func (s *qqSender) retryPassiveReply(
	ctx stdctx.Context,
	req platform.SendRequest,
	lastRes platform.SendResult,
	lastErr error,
) (platform.SendResult, error) {
	// 只有"event_id 无效"值得探测：已过期（40034026）/"该事件不支持回复"
	// （40034027）都是确定性拒绝，重试没有意义，直接返回。
	if qqErrorCode(lastErr) != qqErrEventIDInvalid {
		return lastRes, lastErr
	}

	res, err := lastRes, lastErr

	for _, delay := range s.passiveReplyDelays() {
		if !sleepWithContext(ctx, delay) {
			return res, err // ctx 取消：保留最后一次的错误
		}
		res, err = s.sendOnce(ctx, req)
		if err == nil {
			logger.WithFields(logger.Fields{
				"chat_id":  req.Target.ID,
				"is_group": req.Target.IsGroup,
				"event_id": passiveEventIDValue(req),
				"delay_ms": delay.Milliseconds(),
			}).Warn("[qq.Sender] event_id 被拒后重试成功（平台登记该事件存在滞后，" +
				"首次回复被拒属常态）")
			return res, nil
		}
		// 探测途中若出现非"event_id 无效"的错误（如内容违规），立即停止探测。
		if qqErrorCode(err) != qqErrEventIDInvalid {
			return res, err
		}
		// 每次重试失败都记一条：生产 webhook 路径据此还原"事件到达后多久平台才开始
		// 接受该 event_id"的滞后曲线（[0s 失败, D1 失败, D2 成功] 中的滞留点）。
		logger.WithFields(logger.Fields{
			"chat_id":  req.Target.ID,
			"is_group": req.Target.IsGroup,
			"event_id": passiveEventIDValue(req),
			"delay_ms": delay.Milliseconds(),
			"code":     qqErrorCode(err),
		}).Debug("[qq.Sender] event_id 被动回复重试仍未通过")
	}
	return res, err
}

// sleepWithContext 等待 d，ctx 结束时返回 false。
func sleepWithContext(ctx stdctx.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
