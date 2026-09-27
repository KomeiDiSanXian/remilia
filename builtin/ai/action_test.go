package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/KomeiDiSanXian/remilia/builtin/ai/catalog"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/protocol"
	"github.com/KomeiDiSanXian/remilia/builtin/ai/toolkit"
)

// TestSelectionClassMapping 冻结"未显式声明时按类别派生"的兼容语义：
// 空类别与含 general 均为默认保留，其余为可选，与旧 isGeneralTool 逐一对应。
func TestSelectionClassMapping(t *testing.T) {
	cases := []struct {
		name       string
		categories []string
		want       toolkit.SelectionClass
	}{
		{"nil categories", nil, toolkit.SelectionBaseline},
		{"empty categories", []string{}, toolkit.SelectionBaseline},
		{"general", []string{"general"}, toolkit.SelectionBaseline},
		{"general with others", []string{"web", "general"}, toolkit.SelectionBaseline},
		{"non general", []string{"weather"}, toolkit.SelectionOptional},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := toolkit.Tool{Name: "t", Categories: tc.categories}
			assert.Equal(t, tc.want, toolkit.SelectionClassOf(tool))
			// 翻译必须与既有概念完全等价，保证工具集组成不变。
			assert.Equal(t, toolkit.IsGeneralTool(tool), toolkit.SelectionClassOf(tool).KeepsWhenNoAction())
		})
	}
}

// TestSelectionClassExplicitOverridesCategories 冻结显式声明的优先级：
// 声明了 Selection 的工具不再受类别派生影响，SelectionMandatory 因此可达。
func TestSelectionClassExplicitOverridesCategories(t *testing.T) {
	cases := []struct {
		name string
		tool toolkit.Tool
		want toolkit.SelectionClass
	}{
		{"显式强制保留覆盖 general", toolkit.Tool{Name: "t", Categories: []string{toolkit.CategoryGeneral}, Selection: new(toolkit.SelectionMandatory)}, toolkit.SelectionMandatory},
		{"显式可选覆盖 general", toolkit.Tool{Name: "t", Categories: []string{toolkit.CategoryGeneral}, Selection: new(toolkit.SelectionOptional)}, toolkit.SelectionOptional},
		{"显式默认保留覆盖非 general", toolkit.Tool{Name: "t", Categories: []string{"web"}, Selection: new(toolkit.SelectionBaseline)}, toolkit.SelectionBaseline},
		{"nil 声明按类别派生（通用）", toolkit.Tool{Name: "t", Categories: []string{toolkit.CategoryGeneral}}, toolkit.SelectionBaseline},
		{"nil 声明按类别派生（非通用）", toolkit.Tool{Name: "t", Categories: []string{"web"}}, toolkit.SelectionOptional},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, toolkit.SelectionClassOf(tc.tool))
			assert.Equal(t, tc.want, toolkit.ActionPolicyOf(tc.tool).Selection)
		})
	}
}

// TestSelectionClassKeepsWhenNoAction 冻结各保留级别在"本轮无需主动动作"时的行为：
// 只有可选级别会被抑制，默认保留与强制保留都必须留下（否则击穿前缀缓存）。
func TestSelectionClassKeepsWhenNoAction(t *testing.T) {
	assert.False(t, toolkit.SelectionOptional.KeepsWhenNoAction())
	assert.True(t, toolkit.SelectionBaseline.KeepsWhenNoAction())
	assert.True(t, toolkit.SelectionMandatory.KeepsWhenNoAction())
}

// TestSelectionClassString 冻结保留级别的可读名称（用于日志与断言）。
func TestSelectionClassString(t *testing.T) {
	assert.Equal(t, "optional", toolkit.SelectionOptional.String())
	assert.Equal(t, "baseline", toolkit.SelectionBaseline.String())
	assert.Equal(t, "mandatory", toolkit.SelectionMandatory.String())
	assert.Equal(t, "unknown", toolkit.SelectionClass(200).String())
}

// TestActionSpecAndPolicyPreserveTool 校验派生是无损的：
// 描述与策略必须逐字段等于工具原值，派生层不得偷偷改写语义。
func TestActionSpecAndPolicyPreserveTool(t *testing.T) {
	tool := toolkit.Tool{
		Name:                  "danger",
		Description:           "desc",
		Categories:            []string{"admin"},
		Parameters:            protocol.ToolParamSchema{Type: "object"},
		RequiresApproval:      true,
		AlwaysRequireApproval: true,
		GrantsSendTo:          true,
		Permissions:           []string{"a.b"},
	}

	spec := toolkit.ActionSpecOf(tool)
	assert.Equal(t, tool.Name, spec.Name)
	assert.Equal(t, tool.Description, spec.Description)
	assert.Equal(t, tool.Parameters, spec.Parameters)
	assert.Equal(t, tool.Categories, spec.Categories)

	policy := toolkit.ActionPolicyOf(tool)
	assert.Equal(t, toolkit.SelectionOptional, policy.Selection)
	assert.True(t, policy.RequiresApproval)
	assert.True(t, policy.AlwaysRequireApproval)
	assert.True(t, policy.GrantsSendTo)
	assert.Equal(t, tool.Permissions, policy.Permissions)
	assert.NotContains(t, spec.Categories, "general")
	assert.Contains(t, spec.Categories, "admin")
}

// TestRegistryRoundTripsActionAndTool 冻结注册表两种视图的保真性：
// 注册表内部保存动作视图，Get 还原的执行视图必须逐字段等于注册时的工具，
// 否则投影丢字段会静默改变选择、策略或执行行为。
func TestRegistryRoundTripsActionAndTool(t *testing.T) {
	tool := toolkit.Tool{
		Name:                  "danger",
		Description:           "desc",
		Categories:            []string{"admin", "web"},
		Selection:             new(toolkit.SelectionMandatory),
		GrantsSendTo:          true,
		Parameters:            protocol.ToolParamSchema{Type: "object", Properties: map[string]protocol.ToolParamSchema{"a": {Type: "string"}}},
		RequiresApproval:      true,
		AlwaysRequireApproval: true,
		Permissions:           []string{"a.b"},
		Execute: func(context.Context, map[string]any) (string, error) {
			return "ok", nil
		},
	}

	reg := toolkit.NewToolRegistry()
	reg.Register(tool)

	got, ok := reg.Get(tool.Name)
	require.True(t, ok)
	assert.Equal(t, tool.Name, got.Name)
	assert.Equal(t, tool.Description, got.Description)
	assert.Equal(t, tool.Categories, got.Categories)
	require.NotNil(t, got.Selection, "显式保留级别必须随执行视图返回")
	assert.Equal(t, toolkit.SelectionMandatory, *got.Selection)
	assert.Equal(t, tool.Parameters, got.Parameters)
	assert.Equal(t, tool.RequiresApproval, got.RequiresApproval)
	assert.Equal(t, tool.AlwaysRequireApproval, got.AlwaysRequireApproval)
	assert.Equal(t, tool.GrantsSendTo, got.GrantsSendTo)
	assert.Equal(t, tool.Permissions, got.Permissions)
	require.NotNil(t, got.Execute, "执行载荷必须随执行视图返回")
	out, err := got.Execute(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", out)

	action, ok := reg.Action(tool.Name)
	require.True(t, ok)
	assert.Equal(t, toolkit.ActionOf(tool), action)
	assert.Equal(t, []toolkit.Action{toolkit.ActionOf(tool)}, reg.Actions())

	// 未声明保留级别：还原后仍为 nil（继续按类别派生），不因往返被固化成显式级别。
	reg.Register(toolkit.Tool{Name: "plain", Categories: []string{"web"}})
	plain, ok := reg.Get("plain")
	require.True(t, ok)
	assert.Nil(t, plain.Selection, "未声明的保留级别必须保持 nil")
	assert.Equal(t, toolkit.SelectionOptional, toolkit.SelectionClassOf(plain))
}

// TestOpenAIToolsUseActionSpec 校验协议层序列化从动作描述取值（真实消费者）。
func TestOpenAIToolsUseActionSpec(t *testing.T) {
	tools := []toolkit.Tool{{
		Name:        "echo",
		Description: "回显",
		Parameters:  protocol.ToolParamSchema{Type: "object"},
	}}
	got := toOpenAITools(toolkit.ActionsOf(tools))
	require.Len(t, got, 1)
	assert.Equal(t, "function", got[0].Type)
	assert.Equal(t, "echo", got[0].Function.Name)
	assert.Equal(t, "回显", got[0].Function.Description)
	assert.Equal(t, protocol.ToolParamSchema{Type: "object"}, got[0].Function.Parameters)

	anth := toAnthropicTools(toolkit.ActionsOf(tools))
	require.Len(t, anth, 1)
	assert.Equal(t, "echo", anth[0].Name)
	assert.Equal(t, protocol.ToolParamSchema{Type: "object"}, anth[0].InputSchema)
}

// TestPlanToolsAreBaseline 校验内置规划工具的保留级别仍为默认保留
// （旧语义为 general 必保，重构后不得降级为可选）。
func TestPlanToolsAreBaseline(t *testing.T) {
	for _, tool := range catalog.BuildPlanTools(8) {
		assert.Equal(t, toolkit.SelectionBaseline, toolkit.SelectionClassOf(tool), "tool %q", tool.Name)
	}
}
