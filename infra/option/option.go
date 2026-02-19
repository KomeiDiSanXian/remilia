// Package option provides generic option pattern utilities.
//
// The option pattern is a common Go idiom for configuring structs
// with optional parameters. This package provides generic helpers
// to make option patterns more reusable.
package option

// Option is a generic option function that modifies a configuration.
//
// Example:
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

// Apply applies all options to the target in order.
//
// This is the recommended way to apply options to a configuration struct.
//
// Example:
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

// ApplyNew creates a new instance and applies options to it.
//
// Example:
//
//	cfg := option.ApplyNew(Config{Name: "default"},
//	    WithName("custom"),
//	    WithTimeout(5 * time.Second),
//	)
func ApplyNew[T any](initial T, options ...Option[T]) T {
	Apply(&initial, options...)
	return initial
}

// Conditional returns the option only if the condition is true.
//
// This is useful for conditionally applying options based on runtime conditions.
//
// Example:
//
//	cfg := &Config{}
//	option.Apply(cfg,
//	    WithName("app"),
//	    option.Conditional(debugMode, WithTimeout(time.Hour)), // Only if debugMode
//	    option.Conditional(!production, WithVerbose(true)),    // Only if not production
//	)
func Conditional[T any](condition bool, opt Option[T]) Option[T] {
	if condition {
		return opt
	}
	return func(*T) {} // No-op
}

// Compose combines multiple options into a single option.
//
// This is useful for creating reusable option sets.
//
// Example:
//
//	// Define a set of production options
//	productionOpts := option.Compose(
//	    WithTimeout(30 * time.Second),
//	    WithRetries(3),
//	    WithLogging(false),
//	)
//
//	// Use the composed option
//	cfg := &Config{}
//	option.Apply(cfg, productionOpts)
func Compose[T any](options ...Option[T]) Option[T] {
	return func(t *T) {
		Apply(t, options...)
	}
}

// With creates a simple option that sets a value using a setter function.
//
// Example:
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

// WithDefault creates an option that only sets the value if the current value is zero.
//
// Example:
//
//	cfg := &Config{Name: "existing"}
//	option.Apply(cfg,
//	    option.WithDefault(func(c *Config) *string { return &c.Name }, "default"),
//	)
//	// cfg.Name is still "existing" because it was not zero
func WithDefault[T any, V comparable](getter func(*T) *V, value V) Option[T] {
	return func(t *T) {
		field := getter(t)
		var zero V
		if *field == zero {
			*field = value
		}
	}
}

// Chain returns an option that applies multiple options in sequence.
//
// This is an alias for Compose, provided for clarity in certain contexts.
func Chain[T any](options ...Option[T]) Option[T] {
	return Compose(options...)
}

// NoOp returns an option that does nothing.
//
// This is useful as a placeholder or in conditional logic.
func NoOp[T any]() Option[T] {
	return func(*T) {}
}

// When returns opt if condition is true, otherwise returns NoOp.
//
// This is a convenience function that's equivalent to Conditional
// but can be more readable in some contexts.
func When[T any](condition bool, opt Option[T]) Option[T] {
	if condition {
		return opt
	}
	return NoOp[T]()
}

// Unless returns opt if condition is false, otherwise returns NoOp.
//
// Example:
//
//	option.Apply(cfg,
//	    option.Unless(production, WithDebug(true)),
//	)
func Unless[T any](condition bool, opt Option[T]) Option[T] {
	return When(!condition, opt)
}

// MapOption transforms an option from one type to another.
//
// This is useful when you have options for a nested struct.
//
// Example:
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
//	// Map HTTPConfig option to ServerConfig option
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
