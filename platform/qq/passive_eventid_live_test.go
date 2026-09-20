//go:build network

// 真机验证：event_id 被动回复的当前支持范围、两种写法差异与同值退避重试。
//
// 背景（2026-09 线上故障）：
//   - 官方自动生成文档（/wiki/develop/api-v2/autogen/api/...）对群聊接口的
//     event_id 描述是："被动回复的事件 ID。从事件最外层的id获取。与 msg_id 二选一，
//     支持事件：INTERACTION_CREATE、GROUP_ADD_ROBOT、GROUP_MSG_RECEIVE"。
//     GROUP_MEMBER_ADD 不在清单内，但 2026-08-14 ~ 2026-09-18 实测可用。
//   - 2026-09-18 18:26 起同一代码路径返回 40034025"请求参数event_id无效"；
//     但 2026-09-20 又偶发成功（同一群、同一写法时而成时而败）。
//
// 因此不能靠文档下结论，本测试把判断交给平台本身，分三步取证：
//
//	A. 无效 event_id → 确认错误码形态与 qqEventIDRejected 判定；
//	B. 不带 event_id 的主动消息 → 仅作对照，确认该群是否开启「允许主动在群聊内
//	   发言」（40034105=未开启）；框架**不会**用这条路径兜底被动回复，此用例只
//	   用于说明"为什么不能靠主动消息兜底"。
//	C. 真实 event_id（QQ_TEST_EVENT_ID）→ 原样写法与裸 id 写法各试一次：
//	   2026-09-20 复验结论是原样写法成功、裸 id 被拒 40034025，即写法正确、
//	   40034025 属平台登记的短暂不一致；本用例用于日后复验平台是否变卦。
//	D. 走框架 qqSender.Send：带真实 event_id 时应被平台接受（可能首次踩空、
//	   由 1.5s 退避重试补上，日志会有一条"重试成功"WRN）；带无效 event_id 时
//	   验证重试用尽后**如实返回错误**，不再降级为主动消息。
//
// 运行（PowerShell，从仓库根目录执行；凭据不写死在代码里）：
//
// 凭据解析顺序：环境变量 QQ_APP_ID / QQ_APP_SECRET 优先；未设置时回退读取
// 仓库根目录 config.yaml 的 bot.qq.app_id / bot.qq.secret。
//
//	$env:QQ_TEST_GROUP_ID="<群 openid>"
//	# 可选：从日志里抄一条 5 分钟内的入群事件 id（格式 GROUP_MEMBER_ADD:<uuid>）
//	$env:QQ_TEST_EVENT_ID="GROUP_MEMBER_ADD:<uuid>"
//	go test -tags network -run TestQQEventIDLive -v ./platform/qq/
//
// 注意：用例 C 必须在收到事件的 5 分钟内执行（群聊被动回复有效期 5 分钟），
// 且同一事件最多回复 5 次。
//
// 注意：用例 C（写法 0）与 D 会**真的往群里发消息**（内容带 [remilia 真机验证] 标签），
// 撤回时限只有 2 分钟（超时返回 40064004），请及时撤回或改用专门的测试群。
package qq

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
)

// liveInvalidEventID 是结构合法但不可能对应真实事件的 event_id。
const liveInvalidEventID = "GROUP_MEMBER_ADD:00000000-0000-0000-0000-000000000000"

func TestQQEventIDLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：" +
			"go test -tags network -run TestQQEventIDLive -v ./platform/qq/")
	}
	groupID, _ := resolveLiveTargets(t)
	if groupID == "" {
		t.Skipf("未配置群聊目标：设置 QQ_TEST_GROUP_ID 后运行本测试")
	}

	mgr := token.NewManager(&dto.BotInfo{AppID: appID, AppSecret: appSecret})
	t.Cleanup(mgr.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	require.NoError(t, mgr.WaitReadyWithContext(ctx),
		"获取 QQ access token 失败，请检查 QQ_APP_ID / QQ_APP_SECRET 是否有误")

	api := openapi.New(mgr)
	stamp := time.Now().Format("15:04:05")

	t.Run("A_无效event_id", func(t *testing.T) {
		_, err := api.GroupChat(ctx, groupID, &dto.Message{
			Type:    dto.TextMessage,
			Content: "[remilia 真机验证 A] 无效 event_id，预期被拒 " + stamp,
			EventID: dto.EventID(liveInvalidEventID),
		})
		require.Error(t, err, "无效 event_id 应被平台拒绝")
		t.Logf("平台错误=%v", err)
		t.Logf("识别为 event_id 被拒=%v 错误码=%d（预期 true / 40034025）",
			qqEventIDRejected(err), qqErrorCode(err))
	})

	t.Run("B_主动消息对照", func(t *testing.T) {
		res, err := api.GroupChat(ctx, groupID, &dto.Message{
			Type:    dto.TextMessage,
			Content: "[remilia 真机验证 B] 不带 event_id 的主动消息 " + stamp,
		})
		if err != nil {
			t.Logf("主动消息发送失败：%v", err)
			t.Logf("若为 40034105，说明该群未开启「允许主动在群聊内发言」，" +
				"这正是框架不做主动消息兜底的原因（兜底也会失败）")
			return
		}
		t.Logf("主动消息发送成功 id=%s；仅说明该群允许主动消息，"+
			"框架仍不会用它兜底被动回复", res.Get("id").String())
	})

	t.Run("C_真实event_id两种写法", func(t *testing.T) {
		raw := strings.TrimSpace(os.Getenv("QQ_TEST_EVENT_ID"))
		if raw == "" {
			t.Skipf("未配置 QQ_TEST_EVENT_ID，跳过；请从日志抄一条 5 分钟内的入群事件 id")
		}
		// 默认在第一种写法成功后停止（与生产一致）；设置 QQ_TEST_EVENT_ID_ALL_FORMS=1
		// 则强制把每种写法都试一遍，用于判定"平台是否只认某种写法"。
		tryAll := isTruthy(os.Getenv("QQ_TEST_EVENT_ID_ALL_FORMS"))
		forms := []string{raw}
		if sep := strings.Index(raw, ":"); tryAll && sep > 0 && sep+1 < len(raw) {
			forms = append(forms, raw[sep+1:]) // 裸 id：2026-09-20 复验被拒，仅作对照
		}
		t.Logf("真实 event_id=%s（候选写法 %v，全部尝试=%v）", raw, forms, tryAll)
		for i, form := range forms {
			_, err := api.GroupChat(ctx, groupID, &dto.Message{
				Type:    dto.TextMessage,
				Content: "[remilia 真机验证 C] event_id 写法 " + string(rune('0'+i)) + " " + stamp,
				EventID: dto.EventID(form),
			})
			if err != nil {
				t.Logf("写法 %d（%s）被拒：%v", i, form, err)
				continue
			}
			t.Logf("写法 %d（%s）发送成功 → 平台接受该写法（2026-09-20 复验：仅写法 0 可用）", i, form)
			if !tryAll {
				return
			}
		}
		if !tryAll {
			t.Logf("两种写法都被拒 → 该事件类型当前不支持 event_id 被动回复")
		}
	})

	t.Run("D_Send重试链路", func(t *testing.T) {
		// 有真实 event_id 时优先用它：验证生产链路（框架自动填充 event_id）能一次发出去；
		// 否则用无效 id，验证被拒后确实会按退避间隔重试、用尽后如实报错。
		eventID := strings.TrimSpace(os.Getenv("QQ_TEST_EVENT_ID"))
		if eventID == "" {
			eventID = liveInvalidEventID
		}
		res, err := NewSender(api).Send(ctx, platform.SendRequest{
			Target: platform.ChatInfo{
				ID:      groupID,
				IsGroup: true,
				Tokens:  map[string]string{TokenEventID: eventID},
			},
			Message: platform.TextMessage("[remilia 真机验证 D] Sender 链路 event_id=" + eventID + " " + stamp),
		})
		switch {
		case err == nil:
			if eventID == liveInvalidEventID {
				t.Fatalf("无效 event_id 竟然发送成功 id=%s，说明重试对象被改写", res.MessageID)
			}
			t.Logf("Send 成功 id=%s；上方无 WRN 说明 event_id 被动回复一次命中，"+
				"出现「重试成功」WRN 则说明首次踩空、由退避重试补上", res.MessageID)
		case qqErrorCode(err) == 40034025:
			// 仅当用的是无效 id 时才允许走到这里：重试用尽、如实返回被拒原因。
			require.Equal(t, liveInvalidEventID, eventID,
				"真实 event_id 重试后仍被拒，请检查事件是否已过期或平台是否改回不支持该事件")
			t.Logf("无效 event_id 重试用尽后如实返回：%v", err)
		default:
			t.Fatalf("重试链路出现意外错误：%v", err)
		}
	})
}
