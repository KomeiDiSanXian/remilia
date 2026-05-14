// Package job 提供结构化的后台作业系统，与 [builtin/scheduler] 的区别：
//
//   - scheduler：基于 cron 表达式或固定间隔的**周期性**任务（重复执行）
//   - job：面向**一次性**执行、**延迟**触发、**自动重试**和**顺序链**的有状态后台作业
//
// # 核心特性
//
//   - Once(delay, fn)    — 延迟一次性作业（delay=0 立即提交）
//   - Retry(fn, opts...) — 自动重试，支持指数退避 / 固定间隔退避
//   - Chain(fns...)      — 顺序执行作业链，任意步骤失败即停止
//   - Cancel(id)         — 取消尚未开始执行的作业
//   - Wait(ctx, id)      — 等待指定作业完成
//   - Info(id)           — 查询作业状态
//
// # 快速上手
//
//	pm.Register(job.New())
//
//	// 在其他插件 Setup 中：
//	runner := plugin.Service[*job.Plugin](ctx, "job")
//
//	// 延迟 5s 执行
//	id := runner.Once("send-report", func(ctx context.Context) error {
//	    return sendDailyReport(ctx)
//	}, job.WithDelay(5*time.Second))
//
//	// 最多重试 3 次，指数退避
//	id2 := runner.Retry("fetch-data", fetchDataFn,
//	    job.WithMaxRetries(3),
//	    job.WithExponentialBackoff(500*time.Millisecond, 30*time.Second),
//	)
//
//	// 顺序链：三步依次执行，任一失败停止
//	id3 := runner.Chain("onboard",
//	    createUser, sendWelcomeMail, grantPermissions,
//	)
//
//	// 等待完成
//	if err := runner.Wait(ctx, id3); err != nil {
//	    log.Println("onboard failed:", err)
//	}
package job
