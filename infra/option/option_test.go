package option

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type TestConfig struct {
	Name    string
	Port    int
	Timeout time.Duration
	Enabled bool
}

func WithName(name string) Option[TestConfig] {
	return func(c *TestConfig) { c.Name = name }
}

func WithPort(port int) Option[TestConfig] {
	return func(c *TestConfig) { c.Port = port }
}

func WithTimeout(d time.Duration) Option[TestConfig] {
	return func(c *TestConfig) { c.Timeout = d }
}

func WithEnabled(enabled bool) Option[TestConfig] {
	return func(c *TestConfig) { c.Enabled = enabled }
}

// TestApply tests the Apply function
func TestApply(t *testing.T) {
	t.Run("applies single option", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg, WithName("test"))
		assert.Equal(t, "test", cfg.Name)
	})

	t.Run("applies multiple options in order", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg,
			WithName("test"),
			WithPort(8080),
			WithTimeout(5*time.Second),
		)
		assert.Equal(t, "test", cfg.Name)
		assert.Equal(t, 8080, cfg.Port)
		assert.Equal(t, 5*time.Second, cfg.Timeout)
	})

	t.Run("last option wins for same field", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg,
			WithName("first"),
			WithName("second"),
		)
		assert.Equal(t, "second", cfg.Name)
	})

	t.Run("handles nil options", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg,
			WithName("test"),
			nil,
			WithPort(8080),
		)
		assert.Equal(t, "test", cfg.Name)
		assert.Equal(t, 8080, cfg.Port)
	})

	t.Run("no options is safe", func(t *testing.T) {
		cfg := &TestConfig{Name: "default"}
		Apply(cfg)
		assert.Equal(t, "default", cfg.Name)
	})
}

// TestApplyNew tests the ApplyNew function
func TestApplyNew(t *testing.T) {
	t.Run("creates new instance with options", func(t *testing.T) {
		cfg := ApplyNew(TestConfig{Name: "default"},
			WithName("custom"),
			WithPort(9000),
		)
		assert.Equal(t, "custom", cfg.Name)
		assert.Equal(t, 9000, cfg.Port)
	})

	t.Run("preserves initial values", func(t *testing.T) {
		cfg := ApplyNew(TestConfig{
			Name:    "default",
			Timeout: time.Minute,
		},
			WithPort(8080),
		)
		assert.Equal(t, "default", cfg.Name)
		assert.Equal(t, 8080, cfg.Port)
		assert.Equal(t, time.Minute, cfg.Timeout)
	})
}

// TestConditional tests the Conditional function
func TestConditional(t *testing.T) {
	t.Run("applies option when condition is true", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg,
			WithName("base"),
			Conditional(true, WithPort(8080)),
		)
		assert.Equal(t, "base", cfg.Name)
		assert.Equal(t, 8080, cfg.Port)
	})

	t.Run("skips option when condition is false", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg,
			WithName("base"),
			Conditional(false, WithPort(8080)),
		)
		assert.Equal(t, "base", cfg.Name)
		assert.Equal(t, 0, cfg.Port)
	})

	t.Run("multiple conditionals", func(t *testing.T) {
		debugMode := true
		production := false

		cfg := &TestConfig{}
		Apply(cfg,
			WithName("app"),
			Conditional(debugMode, WithTimeout(time.Hour)),
			Conditional(production, WithPort(443)),
			Conditional(!production, WithPort(8080)),
		)

		assert.Equal(t, "app", cfg.Name)
		assert.Equal(t, time.Hour, cfg.Timeout)
		assert.Equal(t, 8080, cfg.Port)
	})
}

// TestCompose tests the Compose function
func TestCompose(t *testing.T) {
	t.Run("composes multiple options", func(t *testing.T) {
		productionOpts := Compose(
			WithPort(443),
			WithTimeout(30*time.Second),
			WithEnabled(true),
		)

		cfg := &TestConfig{}
		Apply(cfg, productionOpts)

		assert.Equal(t, 443, cfg.Port)
		assert.Equal(t, 30*time.Second, cfg.Timeout)
		assert.True(t, cfg.Enabled)
	})

	t.Run("can be combined with other options", func(t *testing.T) {
		baseOpts := Compose(
			WithPort(8080),
			WithTimeout(5*time.Second),
		)

		cfg := &TestConfig{}
		Apply(cfg,
			WithName("app"),
			baseOpts,
			WithEnabled(true),
		)

		assert.Equal(t, "app", cfg.Name)
		assert.Equal(t, 8080, cfg.Port)
		assert.Equal(t, 5*time.Second, cfg.Timeout)
		assert.True(t, cfg.Enabled)
	})
}

// TestWith tests the With function
func TestWith(t *testing.T) {
	t.Run("creates option from setter", func(t *testing.T) {
		WithCustomPort := With(func(c *TestConfig, port int) {
			c.Port = port * 2 // Custom logic
		})

		cfg := &TestConfig{}
		Apply(cfg, WithCustomPort(4000))
		assert.Equal(t, 8000, cfg.Port)
	})
}

// TestWithDefault tests the WithDefault function
func TestWithDefault(t *testing.T) {
	t.Run("sets value when field is zero", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg,
			WithDefault(func(c *TestConfig) *string { return &c.Name }, "default-name"),
		)
		assert.Equal(t, "default-name", cfg.Name)
	})

	t.Run("does not override existing value", func(t *testing.T) {
		cfg := &TestConfig{Name: "existing"}
		Apply(cfg,
			WithDefault(func(c *TestConfig) *string { return &c.Name }, "default-name"),
		)
		assert.Equal(t, "existing", cfg.Name)
	})

	t.Run("works with numbers", func(t *testing.T) {
		cfg := &TestConfig{Port: 8080}
		Apply(cfg,
			WithDefault(func(c *TestConfig) *int { return &c.Port }, 3000),
		)
		assert.Equal(t, 8080, cfg.Port)

		cfg2 := &TestConfig{}
		Apply(cfg2,
			WithDefault(func(c *TestConfig) *int { return &c.Port }, 3000),
		)
		assert.Equal(t, 3000, cfg2.Port)
	})
}

// TestChain tests the Chain function
func TestChain(t *testing.T) {
	t.Run("chains options", func(t *testing.T) {
		chained := Chain(
			WithName("app"),
			WithPort(8080),
		)

		cfg := &TestConfig{}
		Apply(cfg, chained)
		assert.Equal(t, "app", cfg.Name)
		assert.Equal(t, 8080, cfg.Port)
	})
}

// TestNoOp tests the NoOp function
func TestNoOp(t *testing.T) {
	t.Run("does nothing", func(t *testing.T) {
		cfg := &TestConfig{Name: "original"}
		Apply(cfg, NoOp[TestConfig]())
		assert.Equal(t, "original", cfg.Name)
	})
}

// TestWhen tests the When function
func TestWhen(t *testing.T) {
	t.Run("applies when true", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg, When(true, WithPort(8080)))
		assert.Equal(t, 8080, cfg.Port)
	})

	t.Run("skips when false", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg, When(false, WithPort(8080)))
		assert.Equal(t, 0, cfg.Port)
	})
}

// TestUnless tests the Unless function
func TestUnless(t *testing.T) {
	t.Run("applies when false", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg, Unless(false, WithPort(8080)))
		assert.Equal(t, 8080, cfg.Port)
	})

	t.Run("skips when true", func(t *testing.T) {
		cfg := &TestConfig{}
		Apply(cfg, Unless(true, WithPort(8080)))
		assert.Equal(t, 0, cfg.Port)
	})

	t.Run("unless production example", func(t *testing.T) {
		production := false
		cfg := &TestConfig{}
		Apply(cfg,
			Unless(production, WithEnabled(true)),
		)
		assert.True(t, cfg.Enabled)

		production = true
		cfg2 := &TestConfig{}
		Apply(cfg2,
			Unless(production, WithEnabled(true)),
		)
		assert.False(t, cfg2.Enabled)
	})
}

// TestMapOption tests the MapOption function
func TestMapOption(t *testing.T) {
	type HTTPConfig struct {
		Port int
	}

	type ServerConfig struct {
		Name string
		HTTP HTTPConfig
	}

	t.Run("maps nested option", func(t *testing.T) {
		WithHTTPPort := func(port int) Option[HTTPConfig] {
			return func(c *HTTPConfig) { c.Port = port }
		}

		serverOpt := MapOption(
			func(s *ServerConfig) *HTTPConfig { return &s.HTTP },
			WithHTTPPort(8080),
		)

		cfg := &ServerConfig{}
		Apply(cfg, serverOpt)
		assert.Equal(t, 8080, cfg.HTTP.Port)
	})

	t.Run("can be combined with other options", func(t *testing.T) {
		WithHTTPPort := func(port int) Option[HTTPConfig] {
			return func(c *HTTPConfig) { c.Port = port }
		}

		WithServerName := func(name string) Option[ServerConfig] {
			return func(c *ServerConfig) { c.Name = name }
		}

		cfg := &ServerConfig{}
		Apply(cfg,
			WithServerName("myserver"),
			MapOption(
				func(s *ServerConfig) *HTTPConfig { return &s.HTTP },
				WithHTTPPort(9000),
			),
		)
		assert.Equal(t, "myserver", cfg.Name)
		assert.Equal(t, 9000, cfg.HTTP.Port)
	})
}

// TestRealWorldExample demonstrates real-world usage
func TestRealWorldExample(t *testing.T) {
	t.Run("development configuration", func(t *testing.T) {
		developmentOpts := Compose(
			WithPort(8080),
			WithTimeout(time.Hour),
			WithEnabled(true),
		)

		cfg := ApplyNew(TestConfig{Name: "app"},
			developmentOpts,
		)

		assert.Equal(t, "app", cfg.Name)
		assert.Equal(t, 8080, cfg.Port)
		assert.Equal(t, time.Hour, cfg.Timeout)
		assert.True(t, cfg.Enabled)
	})

	t.Run("production configuration with conditionals", func(t *testing.T) {
		isProduction := true
		enableSSL := true

		productionOpts := Compose(
			When(isProduction, WithPort(443)),
			Unless(isProduction, WithPort(8080)),
			When(enableSSL, WithEnabled(true)),
			When(isProduction, WithTimeout(30*time.Second)),
		)

		cfg := ApplyNew(TestConfig{Name: "prod-app"},
			productionOpts,
		)

		assert.Equal(t, "prod-app", cfg.Name)
		assert.Equal(t, 443, cfg.Port)
		assert.Equal(t, 30*time.Second, cfg.Timeout)
		assert.True(t, cfg.Enabled)
	})
}
