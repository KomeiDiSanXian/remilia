// Package option 提供通用的选项模式工具。
//
// 选项模式是 Go 中常见的惯用法，用于通过可选参数配置结构体。
// 本包以 Option[T] 具名类型承载泛型方法（Go 1.27 泛型方法），
// 使选项模式更具复用性。
package option

// Option 是用于修改配置的泛型选项函数。
//
// 示例：
//
//	type Config struct {
//	    Name    string
//	    Timeout time.Duration
//	}
//
//	func WithName(name string) option.Option[Config] {
//	    return func(c *Config) { c.Name = name }
//	}
//
//	func WithTimeout(d time.Duration) option.Option[Config] {
//	    return func(c *Config) { c.Timeout = d }
//	}
//
//	cfg := &Config{}
//	option.ApplyAll(cfg, WithName("test"), WithTimeout(time.Second))
type Option[T any] func(*T)

// Apply 将选项应用到目标对象。
//
// 示例：
//
//	cfg := &Config{Name: "default"}
//	WithName("custom").Apply(cfg)
func (o Option[T]) Apply(target *T) {
	if o != nil {
		o(target)
	}
}

// ApplyAll 按顺序将所有选项应用到目标对象。
//
// 这是将多个选项应用到配置结构体的推荐方式。
//
// 示例：
//
//	cfg := &Config{Name: "default"}
//	option.ApplyAll(cfg,
//	    WithName("custom"),
//	    WithTimeout(5*time.Second),
//	)
func ApplyAll[T any](target *T, options ...Option[T]) {
	for _, opt := range options {
		opt.Apply(target)
	}
}

// ApplyNew 创建新实例并将选项应用到该实例。
//
// 示例：
//
//	cfg := option.ApplyNew(Config{Name: "default"},
//	    WithName("custom"),
//	    WithTimeout(5*time.Second),
//	)
func ApplyNew[T any](initial T, options ...Option[T]) T {
	ApplyAll(&initial, options...)
	return initial
}

// Compose 将当前选项与其它选项合并为单个选项。
//
// 适用于创建可复用的选项集合。
//
// 示例：
//
//	productionOpts := WithTimeout(30 * time.Second).Compose(
//	    WithRetries(3),
//	    WithLogging(false),
//	)
//
//	cfg := &Config{}
//	productionOpts.Apply(cfg)
func (o Option[T]) Compose(others ...Option[T]) Option[T] {
	return func(t *T) {
		o.Apply(t)
		for _, other := range others {
			other.Apply(t)
		}
	}
}

// Chain 按顺序合并当前选项与其它选项，是 Compose 的别名。
func (o Option[T]) Chain(others ...Option[T]) Option[T] {
	return o.Compose(others...)
}

// Conditional 仅在条件为 true 时返回该选项，否则返回 NoOp。
//
// 适用于根据运行时条件有条件地应用选项。
//
// 示例：
//
//	cfg := &Config{}
//	WithTimeout(time.Hour).Conditional(debugMode).Apply(cfg) // 仅当 debugMode 为 true
func (o Option[T]) Conditional(condition bool) Option[T] {
	if condition {
		return o
	}
	return NoOp[T]()
}

// When 若条件为 true 则返回该选项，否则返回 NoOp（Conditional 的别名）。
func (o Option[T]) When(condition bool) Option[T] {
	return o.Conditional(condition)
}

// Unless 若条件为 false 则返回该选项，否则返回 NoOp。
//
// 示例：
//
//	WithDebug(true).Unless(production).Apply(cfg)
func (o Option[T]) Unless(condition bool) Option[T] {
	return o.Conditional(!condition)
}

// Map 将当前选项映射为另一类型 T 的选项（方法自带类型参数，Go 1.27 泛型方法）。
//
// 适用于为嵌套结构体提供选项。
//
// 示例：
//
//	type ServerConfig struct {
//	    HTTP HTTPConfig
//	}
//
//	type HTTPConfig struct {
//	    Port int
//	}
//
//	func WithHTTPPort(port int) option.Option[HTTPConfig] {
//	    return func(c *HTTPConfig) { c.Port = port }
//	}
//
//	// 将 HTTPConfig 选项映射为 ServerConfig 选项
//	serverOpt := WithHTTPPort(8080).Map(func(s *ServerConfig) *HTTPConfig {
//	    return &s.HTTP
//	})
func (opt Option[U]) Map[T any](getter func(*T) *U) Option[T] {
	return func(t *T) {
		nested := getter(t)
		opt.Apply(nested)
	}
}

// With 使用 setter 函数创建一个简单的值设置选项。
//
// 示例：
//
//	WithName := option.With(func(c *Config, name string) {
//	    c.Name = name
//	})
//
//	cfg := &Config{}
//	WithName("test").Apply(cfg)
func With[T any, V any](setter func(*T, V)) func(V) Option[T] {
	return func(value V) Option[T] {
		return func(t *T) {
			setter(t, value)
		}
	}
}

// WithDefault 创建一个仅在当前值为零值时才设置的选项。
//
// 示例：
//
//	cfg := &Config{Name: "existing"}
//	option.ApplyAll(cfg,
//	    option.WithDefault(func(c *Config) *string { return &c.Name }, "default"),
//	)
//	// cfg.Name 仍为 "existing"，因为它不是零值
func WithDefault[T any, V comparable](getter func(*T) *V, value V) Option[T] {
	return func(t *T) {
		field := getter(t)
		var zero V
		if *field == zero {
			*field = value
		}
	}
}

// NoOp 返回一个什么都不做的选项。
//
// 适用于作为占位符或在条件逻辑中使用。
func NoOp[T any]() Option[T] {
	return func(*T) {}
}
