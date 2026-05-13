package main

import (
	"context"
	"fmt"
	"time"

	subscriptionpkg "github.com/KomeiDiSanXian/remilia/builtin/subscription"
)

// demoSource 演示数据源：每次 Poll 返回一条带当前时间戳的条目。
// 用于展示 subscription 框架的推送订阅能力。
type demoSource struct{}

func (s *demoSource) Name() string { return "demo" }

func (s *demoSource) Description() string {
	return "示例数据源（每次 Poll 返回当前时间戳）"
}

func (s *demoSource) Poll(_ context.Context, param string) ([]subscriptionpkg.Item, error) {
	return []subscriptionpkg.Item{{
		ID:    fmt.Sprintf("demo-%d", time.Now().UnixNano()),
		Title: fmt.Sprintf("[demo:%s] 推送时间：%s", param, time.Now().Format("15:04:05")),
		Body:  "这是一条来自 demo 数据源的测试推送内容。",
	}}, nil
}

func (s *demoSource) ValidateParam(param string) error {
	if param == "" {
		return fmt.Errorf("demo source: param must not be empty")
	}
	return nil
}
