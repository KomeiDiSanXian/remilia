package qq

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// fakeEventIDAPI 记录每次发送携带的 event_id 与正文，并按预设错误码拒绝请求。
type fakeEventIDAPI struct {
	openapi.OpenAPI
	eventIDs []string // 每次调用携带的 event_id（空字符串表示未携带）
	contents []string
	errs     []error // 第 n 次调用返回的错误（nil 或缺省表示成功）
}

func (f *fakeEventIDAPI) record(msg *dto.Message) error {
	f.eventIDs = append(f.eventIDs, string(msg.EventID))
	f.contents = append(f.contents, msg.Content)
	if i := len(f.eventIDs) - 1; i < len(f.errs) {
		return f.errs[i]
	}
	return nil
}

func (f *fakeEventIDAPI) GroupChat(_ context.Context, _ string, msg *dto.Message) (gjson.Result, error) {
	if err := f.record(msg); err != nil {
		return gjson.Result{}, err
	}
	return gjson.Parse(`{"id":"msg_1"}`), nil
}

func (f *fakeEventIDAPI) SingleChat(_ context.Context, _ string, msg *dto.Message) (gjson.Result, error) {
	if err := f.record(msg); err != nil {
		return gjson.Result{}, err
	}
	return gjson.Parse(`{"id":"msg_c2c"}`), nil
}

// qqErr 构造与 openapi.doAndCheck 相同形态的错误串。
func qqErr(code int, message string) error {
	return fmt.Errorf(`qq openapi: https://api.bot.qq.com/v2/groups/gid/messages: HTTP 400 code=%d message=%q`,
		code, message)
}

func groupReq(tokens map[string]string) platform.SendRequest {
	return platform.SendRequest{
		Target:  platform.ChatInfo{ID: "group_openid_001", IsGroup: true, Tokens: tokens},
		Message: platform.TextMessage("欢迎加入本群！"),
	}
}

// TestParsePassiveReplyDelays 覆盖 QQ_PASSIVE_REPLY_DELAYS 的解析与边界：
// 空值表示不覆盖（返回 nil），非法值一律报错交由调用方回退默认序列。
func TestParsePassiveReplyDelays(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []time.Duration
		wantErr bool
	}{
		{"未设置", "", nil, false},
		{"仅空白", "   ", nil, false},
		{"默认分辨率", "0.5,1,3", []time.Duration{500 * time.Millisecond, time.Second, 3 * time.Second}, false},
		{"零延迟", "0,0,0,0", []time.Duration{0, 0, 0, 0}, false},
		{"容忍空白", " 0.25 , 0.5 ", []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}, false},
		{"忽略空项", "1,,2", []time.Duration{time.Second, 2 * time.Second}, false},
		{"非法秒数", "abc", nil, true},
		{"负数", "1,-2", nil, true},
		{"超过4个", "0.25,0.5,1,2,4", nil, true},
		{"只有分隔符", ",,", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePassiveReplyDelays(tc.raw)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNewSender_PassiveReplyDelaysFromEnv 验证环境变量覆盖接到发送器上：
// 合法值生效、非法值回退默认序列、未设置时保持默认。
func TestNewSender_PassiveReplyDelaysFromEnv(t *testing.T) {
	t.Run("合法值生效", func(t *testing.T) {
		t.Setenv(passiveReplyDelaysEnv, "0.25,0.5")
		s := NewSender(&fakeEventIDAPI{}).(*qqSender)
		assert.Equal(t, []time.Duration{250 * time.Millisecond, 500 * time.Millisecond}, s.passiveRetryDelays)
	})

	t.Run("非法值回退默认", func(t *testing.T) {
		t.Setenv(passiveReplyDelaysEnv, "abc")
		s := NewSender(&fakeEventIDAPI{}).(*qqSender)
		assert.Empty(t, s.passiveRetryDelays, "非法值不得注入")
		assert.Equal(t, defaultPassiveReplyDelays, s.passiveReplyDelays())
	})

	t.Run("未设置用默认", func(t *testing.T) {
		s := NewSender(&fakeEventIDAPI{}).(*qqSender)
		assert.Empty(t, s.passiveRetryDelays)
		assert.Equal(t, defaultPassiveReplyDelays, s.passiveReplyDelays())
	})
}

// TestQQEventIDRejected 覆盖三个 event_id 相关错误码与常见非目标错误码的判定。
func TestQQEventIDRejected(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"40034025 event_id无效", qqErr(40034025, "请求参数event_id无效"), true},
		{"40034026 event_id已过期", qqErr(40034026, "请求参数event_id已过期"), true},
		{"40034027 该事件不支持回复", qqErr(40034027, "该事件不支持回复消息"), true},
		{"HTTP200 err_code形态", fmt.Errorf(`qq openapi: url: err_code=40034025 message="请求参数event_id无效"`), true},
		{"40034031 msgid已过期", qqErr(40034031, "msgid已经过期,不能回复"), false},
		{"40034105 主动消息无权限", qqErr(40034105, "主动消息发送失败，无权限"), false},
		{"40034006 内容违规", qqErr(40034006, "消息内容违规"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, qqEventIDRejected(tc.err))
		})
	}
}

// TestQQErrorCode 解析错误串中的错误码（code= 与 err_code= 两种形态）。
func TestQQErrorCode(t *testing.T) {
	assert.Equal(t, 40034025, qqErrorCode(qqErr(40034025, "请求参数event_id无效")))
	assert.Equal(t, 0, qqErrorCode(fmt.Errorf("qq openapi: url: HTTP 500")))
	assert.Equal(t, 0, qqErrorCode(nil))
}

// TestSend_EventIDRetryExhaustedReturnsError 复现线上故障：GROUP_MEMBER_ADD 的
// event_id 被平台拒绝（40034025），首次 + 3 次同值退避重试全部失败时必须如实
// 返回错误（原样保留被拒原因），不得静默降级为主动消息——主动消息是另一种消息
// 类型，需要群内另行开关与配额，框架不替插件作者改语义。
func TestSend_EventIDRetryExhaustedReturnsError(t *testing.T) {
	rejected := qqErr(40034025, "请求参数event_id无效")
	fake := &fakeEventIDAPI{errs: []error{rejected, rejected, rejected, rejected}}
	s := &qqSender{api: fake, passiveRetryDelays: []time.Duration{0, 0, 0}}
	tokens := map[string]string{TokenEventID: "GROUP_MEMBER_ADD:47e327c4-d9ec-4c2d-a600-a2581d4c9765"}

	res, err := s.Send(context.Background(), groupReq(tokens))
	require.Error(t, err, "重试全部失败必须报错，不能静默改发主动消息")
	assert.Empty(t, res.MessageID)
	assert.Equal(t, 40034025, qqErrorCode(err), "必须原样返回被拒原因")

	// 首次 + 3 次重试；不做主动消息兜底，所以没有第 5 次（不带 event_id 的）请求。
	require.Len(t, fake.eventIDs, 4, "首次 + 3 次重试，且不做主动消息兜底")
	assert.Equal(t, []string{
		"GROUP_MEMBER_ADD:47e327c4-d9ec-4c2d-a600-a2581d4c9765",
		"GROUP_MEMBER_ADD:47e327c4-d9ec-4c2d-a600-a2581d4c9765",
		"GROUP_MEMBER_ADD:47e327c4-d9ec-4c2d-a600-a2581d4c9765",
		"GROUP_MEMBER_ADD:47e327c4-d9ec-4c2d-a600-a2581d4c9765",
	}, fake.eventIDs, "每次重试都必须复用同一个 event_id")
	assert.Equal(t, "GROUP_MEMBER_ADD:47e327c4-d9ec-4c2d-a600-a2581d4c9765", tokens[TokenEventID],
		"重试不得污染事件的 token")
}

// TestSend_RetryPassiveReplySucceeds 偶发拒绝（线上实测同一 event_id 时而成时而败）：
// 同值重试成功时必须复用 event_id 被动回复，不得降级为主动消息（不消耗主动消息配额）。
func TestSend_RetryPassiveReplySucceeds(t *testing.T) {
	fake := &fakeEventIDAPI{errs: []error{qqErr(40034025, "请求参数event_id无效")}}
	s := &qqSender{api: fake, passiveRetryDelays: []time.Duration{0}}

	_, err := s.Send(context.Background(), groupReq(map[string]string{TokenEventID: "evt-1"}))
	require.NoError(t, err)
	require.Len(t, fake.eventIDs, 2, "同值重试成功后不应再多发任何请求")
	assert.Equal(t, "evt-1", fake.eventIDs[1], "重试必须复用同一个 event_id")
}

// TestSend_ProbeNonEventIDErrorStopsRetry 重试过程中出现与被动授权无关的错误
// （内容违规等）必须立即返回，不再重发。
func TestSend_ProbeNonEventIDErrorStopsRetry(t *testing.T) {
	fake := &fakeEventIDAPI{errs: []error{
		qqErr(40034025, "请求参数event_id无效"),
		qqErr(40034006, "消息内容违规"),
	}}
	s := &qqSender{api: fake, passiveRetryDelays: []time.Duration{0, 0}}

	_, err := s.Send(context.Background(), groupReq(map[string]string{TokenEventID: "evt-1"}))
	require.Error(t, err)
	assert.Equal(t, 40034006, qqErrorCode(err))
	assert.Len(t, fake.eventIDs, 2, "内容违规应终止重试")
}

// TestSend_UnsupportedEventNoRetry 40034027（该事件不支持回复）是确定性拒绝，
// 重试没有意义：只发一次，并如实返回错误（不做主动消息兜底）。
func TestSend_UnsupportedEventNoRetry(t *testing.T) {
	fake := &fakeEventIDAPI{errs: []error{qqErr(40034027, "该事件不支持回复消息")}}
	s := &qqSender{api: fake, passiveRetryDelays: []time.Duration{0, 0}}

	_, err := s.Send(context.Background(), groupReq(map[string]string{TokenEventID: "GROUP_MEMBER_ADD:evt-1"}))
	require.Error(t, err)
	assert.Equal(t, 40034027, qqErrorCode(err))
	require.Len(t, fake.eventIDs, 1, "确定性拒绝不重试，也不降级为主动消息")
	assert.Equal(t, "GROUP_MEMBER_ADD:evt-1", fake.eventIDs[0])
}

// TestSend_EventIDExpiredNoRetry 40034026（event_id 已过期）同样是确定性拒绝：
// 只发一次并返回错误，不重试、不降级为主动消息。
func TestSend_EventIDExpiredNoRetry(t *testing.T) {
	fake := &fakeEventIDAPI{errs: []error{qqErr(40034026, "请求参数event_id已过期")}}
	s := &qqSender{api: fake, passiveRetryDelays: []time.Duration{0, 0, 0}}

	_, err := s.Send(context.Background(), groupReq(map[string]string{TokenEventID: "GROUP_MEMBER_ADD:evt-1"}))
	require.Error(t, err)
	assert.Equal(t, 40034026, qqErrorCode(err))
	assert.Len(t, fake.eventIDs, 1, "已过期不重试，也不降级为主动消息")
}

// TestSend_NoRetryCases 重试范围必须收窄：
//   - 未携带 event_id 时不重发（否则凭空多一条主动消息）；
//   - msg_id 被拒（时效/次数超限）时不重发（否则绕过平台被动回复限制）；
//   - 与被动授权无关的错误（内容违规、主动消息频控等）不重发。
func TestSend_NoRetryCases(t *testing.T) {
	cases := []struct {
		name   string
		tokens map[string]string
		err    error
	}{
		{"无授权时的40034025", nil, qqErr(40034025, "请求参数event_id无效")},
		{"msg_id过期", map[string]string{TokenMsgID: "msg-1"}, qqErr(40034031, "msgid已经过期,不能回复")},
		{"msg_id无效或越权", map[string]string{TokenMsgID: "msg-1"}, qqErr(40034024, "请求参数msg_id无效或越权")},
		{"内容违规", map[string]string{TokenEventID: "evt-1"}, qqErr(40034006, "消息内容违规")},
		{"主动消息超过频控", map[string]string{TokenEventID: "evt-1"}, qqErr(40034100, "主动消息发送超过频控限制")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeEventIDAPI{errs: []error{tc.err}}
			s := &qqSender{api: fake}

			_, err := s.Send(context.Background(), groupReq(tc.tokens))
			require.Error(t, err)
			assert.Len(t, fake.eventIDs, 1, "不应重发")
		})
	}
}
