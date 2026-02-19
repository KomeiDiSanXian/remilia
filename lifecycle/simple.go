package lifecycle

import "context"

// SimpleComponent 是一个简单的组件实现，通过函数来定义生命周期行为
//
// 使用示例：
//
//	comp := v2.NewSimpleComponent(
//	    "my-component",
//	    func(ctx context.Context) error {
//	        // OnStart: 初始化资源
//	        return nil
//	    },
//	    func(ctx context.Context) error {
//	        // OnRun: 运行时逻辑
//	        <-ctx.Done()
//	        return nil
//	    },
//	    func(ctx context.Context) error {
//	        // OnStop: 清理资源
//	        return nil
//	    },
//	)
type SimpleComponent struct {
	name    string
	onStart func(ctx context.Context) error
	onRun   func(ctx context.Context) error
	onStop  func(ctx context.Context) error
}

// NewSimpleComponent 创建一个简单组件
//
// 参数：
//   - name: 组件名称（必须唯一）
//   - onStart: OnStart 实现（可以为 nil）
//   - onRun: OnRun 实现（可以为 nil）
//   - onStop: OnStop 实现（可以为 nil）
func NewSimpleComponent(
	name string,
	onStart func(ctx context.Context) error,
	onRun func(ctx context.Context) error,
	onStop func(ctx context.Context) error,
) *SimpleComponent {
	return &SimpleComponent{
		name:    name,
		onStart: onStart,
		onRun:   onRun,
		onStop:  onStop,
	}
}

func (c *SimpleComponent) Name() string {
	return c.name
}

func (c *SimpleComponent) OnStart(ctx context.Context) error {
	if c.onStart != nil {
		return c.onStart(ctx)
	}
	return nil
}

func (c *SimpleComponent) OnRun(ctx context.Context) error {
	if c.onRun != nil {
		return c.onRun(ctx)
	}
	// 默认行为：等待 context 取消
	<-ctx.Done()
	return nil
}

func (c *SimpleComponent) OnStop(ctx context.Context) error {
	if c.onStop != nil {
		return c.onStop(ctx)
	}
	return nil
}

// ResourceComponent 是一个管理资源的组件实现
//
// 使用示例：
//
//	comp := v2.NewResourceComponent(
//	    "database",
//	    func(ctx context.Context) (interface{}, error) {
//	        // 打开资源
//	        return sql.Open("postgres", dsn)
//	    },
//	    func(ctx context.Context, res interface{}) error {
//	        // 关闭资源
//	        return res.(*sql.DB).Close()
//	    },
//	)
type ResourceComponent struct {
	name     string
	resource any
	acquire  func(ctx context.Context) (any, error)
	release  func(ctx context.Context, res any) error
}

// NewResourceComponent 创建一个资源管理组件
//
// 参数：
//   - name: 组件名称（必须唯一）
//   - acquire: 获取资源的函数（在 OnStart 中调用）
//   - release: 释放资源的函数（在 OnStop 中调用）
func NewResourceComponent(
	name string,
	acquire func(ctx context.Context) (any, error),
	release func(ctx context.Context, res any) error,
) *ResourceComponent {
	return &ResourceComponent{
		name:    name,
		acquire: acquire,
		release: release,
	}
}

func (c *ResourceComponent) Name() string {
	return c.name
}

func (c *ResourceComponent) OnStart(ctx context.Context) error {
	if c.acquire != nil {
		res, err := c.acquire(ctx)
		if err != nil {
			return err
		}
		c.resource = res
	}
	return nil
}

func (c *ResourceComponent) OnRun(ctx context.Context) error {
	// 资源组件默认只是等待停止信号
	<-ctx.Done()
	return nil
}

func (c *ResourceComponent) OnStop(ctx context.Context) error {
	if c.release != nil && c.resource != nil {
		return c.release(ctx, c.resource)
	}
	return nil
}

// Resource 返回管理的资源
func (c *ResourceComponent) Resource() any {
	return c.resource
}
