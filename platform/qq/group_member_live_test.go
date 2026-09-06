//go:build network

// 真机验证：2026-09-03 官方新增的群成员管理接口（群成员列表/单成员信息/
// 群黑名单查询）。
//
// 官方文档声明这批接口"正在内邀接入中，仅白名单机器人可用"（错误码 11253），
// 因此未开通白名单的机器人会拿到 code=11253，此时本测试以 Skip 提示而非失败。
//
// 运行（PowerShell，从仓库根目录执行；凭据不写死在代码里）：
//
// 凭据解析顺序：环境变量 QQ_APP_ID / QQ_APP_SECRET 优先；未设置时回退读取
// 仓库根目录 config.yaml 的 bot.qq.app_id / bot.qq.secret。
//
//	$env:QQ_TEST_GROUP_ID="<群 openid>"       # 群 openid（允许主动消息）
//	go test -tags network -run TestQQGroupMemberAPIsLive -v ./platform/qq/
package qq

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestQQGroupMemberAPIsLive 真机验证群成员列表/单成员信息/群黑名单查询接口。
func TestQQGroupMemberAPIsLive(t *testing.T) {
	appID, appSecret, ok := liveCredentials(t)
	if !ok {
		t.Skipf("未配置凭据，跳过真机测试。设置 QQ_APP_ID / QQ_APP_SECRET 后运行：go test -tags network -run TestQQGroupMemberAPIsLive -v ./platform/qq/")
	}
	groupID, _ := resolveLiveTargets(t)

	mgr := token.NewManager(&dto.BotInfo{AppID: appID, AppSecret: appSecret})
	t.Cleanup(mgr.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, mgr.WaitReadyWithContext(ctx),
		"获取 QQ access token 失败，请检查 QQ_APP_ID / QQ_APP_SECRET 是否有误")

	api := openapi.New(mgr)

	members, firstResult := liveFetchGroupMembers(t, ctx, api, groupID)
	t.Logf("群成员列表首页原始响应=%s", firstResult.Raw)
	t.Logf("群成员列表共拉取 %d 条（每页最多 30，按 next_cursor 翻页）", len(members))

	probeID := ""
	if len(members) > 0 {
		probeID = members[0].Get("member_openid").String()
		t.Logf("首页首位成员：openid=%s username=%s member_role=%s bot=%v",
			probeID, members[0].Get("username").String(), members[0].Get("member_role").String(), members[0].Get("bot").Bool())
	}
	if probeID == "" {
		// 列表为空时退而用机器人在群内的 openid 做单成员查询探测。
		if bs, err := api.GetGroupBotState(ctx, groupID); err == nil {
			probeID = bs.Get("member_openid").String()
		}
	}
	if probeID != "" {
		info, err := api.GetGroupMember(ctx, groupID, probeID)
		require.NoError(t, err, "获取群成员信息失败（若提示 11253 说明机器人未开通白名单）")
		t.Logf("单成员查询成功：openid=%s username=%s member_role=%s joined_at=%s",
			info.Get("member_openid").String(), info.Get("username").String(),
			info.Get("member_role").String(), info.Get("joined_at").String())
	} else {
		t.Logf("群内未查询到可探测成员，跳过单成员信息验证（成员列表可能为空）")
	}

	blacklist, err := api.GetGroupMemberBlacklist(ctx, groupID, "", 100)
	require.NoError(t, err, "群黑名单查询失败（若提示 11253 说明机器人未开通白名单）")
	t.Logf("群黑名单查询成功：本页 %d 条，next_cursor=%q，原始响应=%s",
		len(blacklist.Get("users").Array()), blacklist.Get("next_cursor").String(), blacklist.Raw)
}

// liveFetchGroupMembers 按 cursor 翻页拉取全部群成员；未开通白名单
// （code=11253）时以 Skip 提示。
func liveFetchGroupMembers(t *testing.T, ctx context.Context, api openapi.OpenAPI, groupID string) ([]gjson.Result, gjson.Result) {
	t.Helper()
	var (
		members []gjson.Result
		first   gjson.Result
		cursor  = ""
	)
	for {
		result, err := api.GetGroupMemberList(ctx, groupID, cursor)
		if err != nil {
			if strings.Contains(err.Error(), "11253") {
				t.Skipf("机器人未开通群成员接口白名单（code=11253），跳过真机验证。官方文档：该能力正在内邀接入中。错误：%v", err)
			}
			t.Fatalf("获取群成员列表失败：%v", err)
		}
		if first.Raw == "" {
			first = result
		}
		for _, m := range result.Get("members").Array() {
			members = append(members, m)
		}
		next := result.Get("next_cursor").String()
		if next == "" || next == cursor {
			return members, first
		}
		cursor = next
	}
}
