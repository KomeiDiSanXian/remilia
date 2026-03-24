// Package option 提供通用的选项模式工具函数。
//
// 选项模式是 Go 中常见的惯用法，用于通过可选参数配置结构体。
// 本包提供泛型辅助函数，使选项模式更具复用性。
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
//	option.Apply(cfg, WithName("test"), WithTimeout(time.Second))
type Option[T any] func(*T)

// Apply 按顺序将所有选项应用到目标对象。
//
// 这是将选项应用到配置结构体的推荐方式。
//
// 示例：
//
//	cfg := &Config{Name: "default"}
//	option.Apply(cfg,
//	    WithName("custom"),
//	    WithTimeout(5 * time.Second),
//	)
func Apply[T any](target *T, options ...Option[T]) {
	for _, opt := range options {
		if opt != nil {
			opt(target)
		}
	}
}

// ApplyNew 创建新实例并将选项应用到该实例。
//
// 示例：
//
//	cfg := option.ApplyNew(Config{Name: "default"},
//	    WithName("custom"),
//	    WithTimeout(5 * time.Second),
//	)
func ApplyNew[T any](initial T, options ...Option[T]) T {
	Apply(&initial, options...)
	return initial
}

// Conditional 仅在条件为 true 时返回该选项。
//
// 适用于根据运行时条件有条件地应用选项。
//
// 示例：
//
//	cfg := &Config{}
//	option.Apply(cfg,
//	    WithName("app"),
//	    option.Conditional(debugMode, WithTimeout(time.Hour)), // 仅当 debugMode 为 true
//	    option.Conditional(!production, WithVerbose(true)),    // 仅当非生产环境
//	)
func Conditional[T any](condition bool, opt Option[T]) Option[T] {
	if condition {
		return opt
	}
	return func(*T) {} // No-op
}

// Compose 将多个选项合并为单个选项。
//
// 适用于创建可复用的选项集合。
//
// 示例：
//
//	// 定义一组生产环境选项
//	productionOpts := option.Compose(
//	    WithTimeout(30 * time.Second),
//	    WithRetries(3),
//	    WithLogging(false),
//	)
//
//	// 使用合并后的选项
//	cfg := &Config{}
//	option.Apply(cfg, productionOpts)
func Compose[T any](options ...Option[T]) Option[T] {
	return func(t *T) {
		Apply(t, options...)
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
//	option.Apply(cfg, WithName("test"))
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
//	option.Apply(cfg,
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

// Chain 按顺序依次应用多个选项，返回合并后的单个选项。
//
// 这是 Compose 的别名，在某些场景下更具可读性。
func Chain[T any](options ...Option[T]) Option[T] {
	return Compose(options...)
}

// NoOp 返回一个什么都不做的选项。
//
// 适用于作为占位符或在条件逻辑中使用。
func NoOp[T any]() Option[T] {
	return func(*T) {}
}

// When 若条件为 true 则返回 opt，否则返回 NoOp。
//
// 这是 Conditional 的便捷函数，在某些场景下可读性更好。
func When[T any](condition bool, opt Option[T]) Option[T] {
	if condition {
		return opt
	}
	return NoOp[T]()
}

// Unless 若条件为 false 则返回 opt，否则返回 NoOp。
//
// 示例：
//
//	option.Apply(cfg,
//	    option.Unless(production, WithDebug(true)),
//	)
func Unless[T any](condition bool, opt Option[T]) Option[T] {
	return When(!condition, opt)
}

// MapOption 将一种类型的选项转换为另一种类型的选项。
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
//	serverOpt := option.MapOption(
//	    func(s *ServerConfig) *HTTPConfig { return &s.HTTP },
//	    WithHTTPPort(8080),
//	)
func MapOption[T any, U any](getter func(*T) *U, opt Option[U]) Option[T] {
	return func(t *T) {
		nested := getter(t)
		opt(nested)
	}
}
