//go:build example
// +build example

package main

import (
	"errors"
	"fmt"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/helper"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

func main() {
	fmt.Println("===== Handler 组合示例 =====\n")

	eng := engine.NewEngine()

	// 示例 1: 使用 Chain 组合多个 Handler
	example1_Chain(eng)

	// 示例 2: 使用 ToMiddleware 转换 Handler
	example2_ToMiddleware(eng)

	// 示例 3: 使用 Conditional 条件执行
	example3_Conditional(eng)

	// 示例 4: 混合使用
	example4_Mixed(eng)

	fmt.Println("\n=====完成！=====")
}

// 示例 1: Chain - 链式组合多个 Handler
func example1_Chain(eng *engine.Engine) {
	fmt.Println("示例 1: Chain - 链式组合")

	// 定义多个独立的 Handler
	validateInput := func(ctx *context.Context) error {
		fmt.Println("  步骤 1: 验证输入")
		// 实际应用中会验证参数
		return nil
	}

	parseData := func(ctx *context.Context) error {
		fmt.Println("  步骤 2: 解析数据")
		// 实际应用中会解析请求数据
		return nil
	}

	processLogic := func(ctx *context.Context) error {
		fmt.Println("  步骤 3: 处理业务逻辑")
		// 实际应用中会执行业务逻辑
		return nil
	}

	formatOutput := func(ctx *context.Context) error {
		fmt.Println("  步骤 4: 格式化输出")
		// 实际应用中会格式化响应
		return nil
	}

	// 使用 Chain 组合所有 Handler
	handler := helper.Chain(
		validateInput,
		parseData,
		processLogic,
		formatOutput,
	)

	eng.OnCommand(dto.GroupAtMessageCreate, "/process").Handle(handler)

	fmt.Println("✅ 注册命令 /process（使用 Chain）\n")
}

// 示例 2: ToMiddleware - 将 Handler 转换为中间件
func example2_ToMiddleware(eng *engine.Engine) {
	fmt.Println("示例 2: ToMiddleware - Handler 转换为中间件")

	// 可复用的 Handler
	requireAuth := func(ctx *context.Context) error {
		fmt.Println("  中间件: 验证鉴权")
		// 实际应用中会检查用户权限
		return nil
	}

	logAccess := func(ctx *context.Context) error {
		fmt.Println("  中间件: 记录访问日志")
		// 实际应用中会记录日志
		return nil
	}

	businessLogic := func(ctx *context.Context) error {
		fmt.Println("  Handler: 执行业务逻辑")
		return nil
	}

	// 转换为中间件并使用
	eng.OnCommand(dto.GroupAtMessageCreate, "/admin").
		Use(helper.ToMiddleware(requireAuth)).
		Use(helper.ToMiddleware(logAccess)).
		Handle(businessLogic)

	fmt.Println("✅ 注册命令 /admin（使用 ToMiddleware）\n")
}

// 示例 3: Conditional - 条件执行
func example3_Conditional(eng *engine.Engine) {
	fmt.Println("示例 3: Conditional - 条件执行")

	// 条件检查
	isAdmin := func(ctx *context.Context) error {
		// 模拟：假设我们检查用户是否是管理员
		hasAdmin := false // 实际应用中从 context 获取
		if !hasAdmin {
			return errors.New("not admin")
		}
		return nil
	}

	// 管理员处理器
	adminHandler := func(ctx *context.Context) error {
		fmt.Println("  执行: 管理员面板")
		return nil
	}

	// 普通用户处理器
	userHandler := func(ctx *context.Context) error {
		fmt.Println("  执行: 用户面板")
		return nil
	}

	// 使用条件执行
	handler := helper.Conditional(isAdmin, adminHandler, userHandler)

	eng.OnCommand(dto.GroupAtMessageCreate, "/panel").Handle(handler)

	fmt.Println("✅ 注册命令 /panel（使用 Conditional）\n")
}

// 示例 4: 混合使用 - 组合多种模式
func example4_Mixed(eng *engine.Engine) {
	fmt.Println("示例 4: 混合使用")

	// 复用的验证器
	validateAuth := func(ctx *context.Context) error {
		fmt.Println("  验证: 鉴权")
		return nil
	}

	validateInput := func(ctx *context.Context) error {
		fmt.Println("  验证: 输入参数")
		return nil
	}

	// 业务逻辑步骤
	step1 := func(ctx *context.Context) error {
		fmt.Println("  业务: 步骤 1 - 查询数据")
		return nil
	}

	step2 := func(ctx *context.Context) error {
		fmt.Println("  业务: 步骤 2 - 处理数据")
		return nil
	}

	step3 := func(ctx *context.Context) error {
		fmt.Println("  业务: 步骤 3 - 保存结果")
		return nil
	}

	// 组合业务逻辑
	businessFlow := helper.Chain(step1, step2, step3)

	// 混合使用：中间件 + Handler 链
	eng.OnCommand(dto.GroupAtMessageCreate, "/complex").
		Use(helper.ToMiddleware(validateAuth)).
		Use(helper.ToMiddleware(validateInput)).
		Handle(businessFlow)

	fmt.Println("✅ 注册命令 /complex（混合使用）\n")
}

// 示例 5: 实际应用场景
func example5_RealWorld() {
	fmt.Println("示例 5: 实际应用场景\n")

	eng := engine.NewEngine()

	// 场景: 用户搜索命令
	// 需求: 鉴权 → 解析参数 → 查询数据库 → 过滤结果 → 格式化输出

	validateUser := func(ctx *context.Context) error {
		// 验证用户是否登录
		fmt.Println("  → 验证用户身份")
		return nil
	}

	parseSearchQuery := func(ctx *context.Context) error {
		// 解析搜索关键词
		fmt.Println("  → 解析搜索参数")
		// keyword := ctx.GetArg("keyword")
		// ctx.Set("parsed_keyword", keyword)
		return nil
	}

	queryDatabase := func(ctx *context.Context) error {
		// 查询数据库
		fmt.Println("  → 查询数据库")
		// results := db.Search(ctx.Get("parsed_keyword"))
		// ctx.Set("results", results)
		return nil
	}

	filterResults := func(ctx *context.Context) error {
		// 根据用户权限过滤结果
		fmt.Println("  → 过滤结果")
		return nil
	}

	formatResponse := func(ctx *context.Context) error {
		// 格式化返回
		fmt.Println("  → 格式化响应")
		return nil
	}

	// 组合所有步骤
	searchHandler := helper.Chain(
		validateUser,
		parseSearchQuery,
		queryDatabase,
		filterResults,
		formatResponse,
	)

	eng.OnCommand(dto.GroupAtMessageCreate, "/search").Handle(searchHandler)

	fmt.Println("✅ 实际应用: 搜索命令")
}

// 示例 6: 对比 - 不使用 helper 的传统方式
func example6_Comparison() {
	fmt.Println("\n===== 对比：使用 helper vs 传统方式 =====\n")

	eng := engine.NewEngine()

	// 传统方式：所有逻辑在一个 Handler 中
	fmt.Println("传统方式（单一 Handler）:")
	eng.OnCommand(dto.GroupAtMessageCreate, "/traditional").Handle(
		func(ctx *context.Context) error {
			// 验证
			fmt.Println("  验证输入")
			// 解析
			fmt.Println("  解析数据")
			// 处理
			fmt.Println("  处理逻辑")
			// 格式化
			fmt.Println("  格式化输出")
			return nil
		},
	)
	fmt.Println("❌ 代码混在一起，难以复用\n")

	// 使用 helper：清晰的步骤分离
	fmt.Println("使用 helper（组合 Handler）:")

	validate := func(ctx *context.Context) error {
		fmt.Println("  验证输入")
		return nil
	}
	parse := func(ctx *context.Context) error {
		fmt.Println("  解析数据")
		return nil
	}
	process := func(ctx *context.Context) error {
		fmt.Println("  处理逻辑")
		return nil
	}
	format := func(ctx *context.Context) error {
		fmt.Println("  格式化输出")
		return nil
	}

	eng.OnCommand(dto.GroupAtMessageCreate, "/modern").Handle(
		helper.Chain(validate, parse, process, format),
	)
	fmt.Println("✅ 步骤清晰，每个函数可独立复用\n")
}
