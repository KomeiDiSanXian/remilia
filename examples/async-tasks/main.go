package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/config"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/platform"
	qq "github.com/KomeiDiSanXian/remilia/platform/qq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// 异步任务处理示例
// 展示如何处理长时间运行的异步任务

type Task struct {
	ID        string
	UserID    string
	Status    string
	Progress  int
	Result    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TaskManager struct {
	tasks map[string]*Task
	mu    sync.RWMutex
}

func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks: make(map[string]*Task),
	}
}

func (tm *TaskManager) CreateTask(userID string) *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task := &Task{
		ID:        fmt.Sprintf("task-%d", time.Now().Unix()),
		UserID:    userID,
		Status:    "pending",
		Progress:  0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	tm.tasks[task.ID] = task
	return task
}

func (tm *TaskManager) GetTask(taskID string) (*Task, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	task, exists := tm.tasks[taskID]
	return task, exists
}

func (tm *TaskManager) UpdateTask(taskID string, status string, progress int, result string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if task, exists := tm.tasks[taskID]; exists {
		task.Status = status
		task.Progress = progress
		task.Result = result
		task.UpdatedAt = time.Now()
	}
}

var taskManager = NewTaskManager()

func main() {
	// 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化日志
	logCfg := logger.Config{
		Level:      cfg.Log.Level,
		Console:    true,
		File:       false,
		TimeFormat: "2006-01-02 15:04:05",
	}
	if err := logger.Init(logCfg); err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}

	// 创建 BotInfo
	botInfo := &dto.BotInfo{
		QQNum:     cfg.Bot.QQ.BotID,
		AppID:     cfg.Bot.QQ.AppID,
		Token:     cfg.Bot.QQ.Token,
		AppSecret: cfg.Bot.QQ.Secret,
	}

	// 创建 Bot
	adapter := qq.NewWebhookServerAdapter(":8080", botInfo)
	bot, err := remilia.NewBotBuilder().
		WithPlatformAdapter(adapter).
		WithName("async-tasks").
		Build()
	if err != nil {
		log.Fatalf("Failed to build bot: %v", err)
	}

	// 使用开发环境中间件
	bot.Engine().Use(middleware.DevelopmentSet()...)

	// 注册处理器
	registerHandlers(bot)

	logger.Info("[AsyncTasks] Bot started! Try these commands:")
	logger.Info("[AsyncTasks] /start - 启动异步任务")
	logger.Info("[AsyncTasks] /status <task_id> - 查询任务状态")
	logger.Info("[AsyncTasks] /list - 列出所有任务")

	bot.Start()
	bot.WaitForShutdown()
}

func registerHandlers(bot *remilia.Bot) {
	// 启动异步任务
	bot.Engine().OnCommand("", "/start").Handle(func(ctx *eventctx.Context) error {
		userID := ctx.GetSenderInfo().ID

		// 创建任务
		task := taskManager.CreateTask(userID)

		// 立即响应用户
		ctx.Reply(platform.TextMessage(fmt.Sprintf("✅ 任务已创建\n任务ID: %s\n\n使用 /status %s 查询进度", task.ID, task.ID)))

		// 异步执行任务
		go executeAsyncTask(task.ID)

		return nil
	})

	// 查询任务状态
	bot.Engine().OnCommand("", "/status").Handle(func(ctx *eventctx.Context) error {

		// 简化：从Content中提取task_id（实际应该用命令解析）
		// 这里假设用户输入 "/status task-xxx"
		taskID := "task-" // 简化演示

		task, exists := taskManager.GetTask(taskID)
		if !exists {
			ctx.Reply(platform.TextMessage("❌ 任务不存在\n使用 /list 查看所有任务"))
			return nil
		}

		ctx.Reply(platform.TextMessage(formatTaskStatus(task)))
		return nil
	})

	// 列出所有任务
	bot.Engine().OnCommand("", "/list").Handle(func(ctx *eventctx.Context) error {
		userID := ctx.GetSenderInfo().ID

		// 获取所有任务
		taskManager.mu.RLock()
		tasks := make([]*Task, 0, len(taskManager.tasks))
		for _, task := range taskManager.tasks {
			if task.UserID == userID {
				tasks = append(tasks, task)
			}
		}
		taskManager.mu.RUnlock()

		if len(tasks) == 0 {
			ctx.Reply(platform.TextMessage("📭 暂无任务"))
			return nil
		}

		var content strings.Builder
		content.WriteString("📋 你的任务列表:\n\n")
		for _, task := range tasks {
			content.WriteString(fmt.Sprintf("• %s: %s (%d%%)\n", task.ID, task.Status, task.Progress))
		}

		ctx.Reply(platform.TextMessage(content.String()))
		return nil
	})

	logger.Info("[AsyncTasks] Handlers registered")
}

// executeAsyncTask 执行异步任务
func executeAsyncTask(taskID string) {
	logger.WithFields(logger.Fields{
		"task_id": taskID,
	}).Info("[AsyncTasks] Task started")

	// 更新为运行中
	taskManager.UpdateTask(taskID, "running", 0, "")

	// 模拟长时间任务（分步执行）
	steps := []string{
		"初始化...",
		"处理数据...",
		"计算结果...",
		"生成报告...",
		"完成",
	}

	for i, step := range steps {
		// 模拟每个步骤的处理时间
		time.Sleep(2 * time.Second)

		progress := (i + 1) * 20
		logger.WithFields(logger.Fields{
			"task_id":  taskID,
			"step":     step,
			"progress": progress,
		}).Info("[AsyncTasks] Task progress")

		taskManager.UpdateTask(taskID, "running", progress, step)
	}

	// 任务完成
	result := "任务执行成功！"
	taskManager.UpdateTask(taskID, "completed", 100, result)

	logger.WithFields(logger.Fields{
		"task_id": taskID,
	}).Info("[AsyncTasks] Task completed")
}

// formatTaskStatus 格式化任务状态
func formatTaskStatus(task *Task) string {
	status := "📊 任务状态\n\n"
	status += fmt.Sprintf("任务ID: %s\n", task.ID)
	status += fmt.Sprintf("状态: %s\n", task.Status)
	status += fmt.Sprintf("进度: %d%%\n", task.Progress)

	if task.Result != "" {
		status += fmt.Sprintf("结果: %s\n", task.Result)
	}

	status += fmt.Sprintf("创建时间: %s\n", task.CreatedAt.Format("15:04:05"))
	status += fmt.Sprintf("更新时间: %s", task.UpdatedAt.Format("15:04:05"))

	return status
}
