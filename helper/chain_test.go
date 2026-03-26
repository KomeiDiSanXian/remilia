package helper

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestChainGeneric tests the generic ChainGeneric function
func TestChainGeneric(t *testing.T) {
	t.Run("empty chain returns nil error", func(t *testing.T) {
		handler := ChainGeneric[int]()
		err := handler(42)
		assert.NoError(t, err)
	})

	t.Run("single handler", func(t *testing.T) {
		called := false
		handler := ChainGeneric(func(x int) error {
			called = true
			assert.Equal(t, 42, x)
			return nil
		})
		err := handler(42)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("multiple handlers execute in order", func(t *testing.T) {
		order := []int{}
		h1 := func(x int) error {
			order = append(order, 1)
			return nil
		}
		h2 := func(x int) error {
			order = append(order, 2)
			return nil
		}
		h3 := func(x int) error {
			order = append(order, 3)
			return nil
		}

		handler := ChainGeneric(h1, h2, h3)
		err := handler(42)
		assert.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, order)
	})

	t.Run("stops on first error", func(t *testing.T) {
		executed := []int{}
		h1 := func(x int) error {
			executed = append(executed, 1)
			return nil
		}
		h2 := func(x int) error {
			executed = append(executed, 2)
			return errors.New("error from h2")
		}
		h3 := func(x int) error {
			executed = append(executed, 3)
			return nil
		}

		handler := ChainGeneric(h1, h2, h3)
		err := handler(42)
		assert.Error(t, err)
		assert.Equal(t, "error from h2", err.Error())
		assert.Equal(t, []int{1, 2}, executed)
	})

	t.Run("works with string type", func(t *testing.T) {
		result := ""
		h1 := func(s string) error {
			result += s
			return nil
		}
		h2 := func(s string) error {
			result += " world"
			return nil
		}

		handler := ChainGeneric(h1, h2)
		err := handler("hello")
		assert.NoError(t, err)
		assert.Equal(t, "hello world", result)
	})
}

// TestChainWithNext tests the middleware-style chain
func TestChainWithNext(t *testing.T) {
	t.Run("empty chain calls next", func(t *testing.T) {
		called := false
		chain := ChainWithNext[int]()
		err := chain(42, func(x int) error {
			called = true
			return nil
		})
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("middleware can wrap next", func(t *testing.T) {
		order := []string{}
		mw := func(x int, next func(int) error) error {
			order = append(order, "before")
			err := next(x)
			order = append(order, "after")
			return err
		}

		chain := ChainWithNext(mw)
		err := chain(42, func(x int) error {
			order = append(order, "handler")
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, []string{"before", "handler", "after"}, order)
	})

	t.Run("multiple middlewares execute in order", func(t *testing.T) {
		order := []int{}
		mw1 := func(x int, next func(int) error) error {
			order = append(order, 1)
			err := next(x)
			order = append(order, -1)
			return err
		}
		mw2 := func(x int, next func(int) error) error {
			order = append(order, 2)
			err := next(x)
			order = append(order, -2)
			return err
		}

		chain := ChainWithNext(mw1, mw2)
		err := chain(42, func(x int) error {
			order = append(order, 99)
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, []int{1, 2, 99, -2, -1}, order)
	})

	t.Run("middleware can skip next", func(t *testing.T) {
		nextCalled := false
		mw := func(x int, next func(int) error) error {
			// Don't call next
			return errors.New("short circuit")
		}

		chain := ChainWithNext(mw)
		err := chain(42, func(x int) error {
			nextCalled = true
			return nil
		})

		assert.Error(t, err)
		assert.False(t, nextCalled)
	})
}

// TestPipe tests the Pipe function
func TestPipe(t *testing.T) {
	t.Run("empty pipe returns input", func(t *testing.T) {
		pipe := Pipe[int]()
		result := pipe(42)
		assert.Equal(t, 42, result)
	})

	t.Run("single function", func(t *testing.T) {
		pipe := Pipe(func(x int) int { return x * 2 })
		result := pipe(21)
		assert.Equal(t, 42, result)
	})

	t.Run("multiple functions compose correctly", func(t *testing.T) {
		pipe := Pipe(
			func(x int) int { return x + 1 }, // 42 -> 43
			func(x int) int { return x * 2 }, // 43 -> 86
			func(x int) int { return x - 2 }, // 86 -> 84
		)
		result := pipe(42)
		assert.Equal(t, 84, result)
	})

	t.Run("string transformation pipeline", func(t *testing.T) {
		slugify := Pipe(
			strings.TrimSpace,
			strings.ToLower,
			func(s string) string { return strings.ReplaceAll(s, " ", "-") },
		)
		result := slugify("  Hello World  ")
		assert.Equal(t, "hello-world", result)
	})
}

// TestCompose tests the Compose function
func TestCompose(t *testing.T) {
	t.Run("empty compose returns input", func(t *testing.T) {
		composed := Compose[int]()
		result := composed(42)
		assert.Equal(t, 42, result)
	})

	t.Run("single function", func(t *testing.T) {
		composed := Compose(func(x int) int { return x * 2 })
		result := composed(21)
		assert.Equal(t, 42, result)
	})

	t.Run("composes in reverse order of Pipe", func(t *testing.T) {
		// Compose: f(g(h(x)))
		// Same functions as Pipe test but reversed order
		composed := Compose(
			func(x int) int { return x - 2 }, // Applied third: 86 - 2 = 84
			func(x int) int { return x * 2 }, // Applied second: 43 * 2 = 86
			func(x int) int { return x + 1 }, // Applied first: 42 + 1 = 43
		)
		result := composed(42)
		assert.Equal(t, 84, result) // Should be 84 like Pipe test
	})
}

// TestFilter tests the generic Filter function
func TestFilter(t *testing.T) {
	t.Run("filters even numbers", func(t *testing.T) {
		numbers := []int{1, 2, 3, 4, 5, 6}
		evens := Filter(numbers, func(n int) bool { return n%2 == 0 })
		assert.Equal(t, []int{2, 4, 6}, evens)
	})

	t.Run("empty slice", func(t *testing.T) {
		numbers := []int{}
		result := Filter(numbers, func(n int) bool { return true })
		assert.Equal(t, []int{}, result)
	})

	t.Run("no matches", func(t *testing.T) {
		numbers := []int{1, 3, 5}
		evens := Filter(numbers, func(n int) bool { return n%2 == 0 })
		assert.Equal(t, []int{}, evens)
	})

	t.Run("works with strings", func(t *testing.T) {
		words := []string{"hello", "world", "foo", "bar"}
		long := Filter(words, func(s string) bool { return len(s) > 3 })
		assert.Equal(t, []string{"hello", "world"}, long)
	})
}

// TestMap tests the generic Map function
func TestMap(t *testing.T) {
	t.Run("doubles numbers", func(t *testing.T) {
		numbers := []int{1, 2, 3}
		doubled := Map(numbers, func(n int) int { return n * 2 })
		assert.Equal(t, []int{2, 4, 6}, doubled)
	})

	t.Run("empty slice", func(t *testing.T) {
		numbers := []int{}
		result := Map(numbers, func(n int) int { return n * 2 })
		assert.Equal(t, []int{}, result)
	})

	t.Run("changes type", func(t *testing.T) {
		numbers := []int{1, 2, 3}
		s := Map(numbers, func(n int) string {
			return "num" + string(rune('0'+n))
		})
		assert.Equal(t, []string{"num1", "num2", "num3"}, s)
	})
}

// TestReduce tests the generic Reduce function
func TestReduce(t *testing.T) {
	t.Run("sum of numbers", func(t *testing.T) {
		numbers := []int{1, 2, 3, 4}
		sum := Reduce(numbers, 0, func(acc, n int) int { return acc + n })
		assert.Equal(t, 10, sum)
	})

	t.Run("product of numbers", func(t *testing.T) {
		numbers := []int{2, 3, 4}
		product := Reduce(numbers, 1, func(acc, n int) int { return acc * n })
		assert.Equal(t, 24, product)
	})

	t.Run("empty slice returns initial", func(t *testing.T) {
		numbers := []int{}
		result := Reduce(numbers, 42, func(acc, n int) int { return acc + n })
		assert.Equal(t, 42, result)
	})

	t.Run("concatenate strings", func(t *testing.T) {
		words := []string{"hello", " ", "world"}
		result := Reduce(words, "", func(acc, s string) string { return acc + s })
		assert.Equal(t, "hello world", result)
	})
}

// TestFind tests the generic Find function
func TestFind(t *testing.T) {
	t.Run("finds first match", func(t *testing.T) {
		numbers := []int{1, 2, 3, 4, 5}
		found, ok := Find(numbers, func(n int) bool { return n > 3 })
		assert.True(t, ok)
		assert.Equal(t, 4, found)
	})

	t.Run("no match returns zero value", func(t *testing.T) {
		numbers := []int{1, 2, 3}
		found, ok := Find(numbers, func(n int) bool { return n > 10 })
		assert.False(t, ok)
		assert.Equal(t, 0, found)
	})

	t.Run("empty slice", func(t *testing.T) {
		numbers := []int{}
		found, ok := Find(numbers, func(n int) bool { return true })
		assert.False(t, ok)
		assert.Equal(t, 0, found)
	})

	t.Run("works with strings", func(t *testing.T) {
		words := []string{"foo", "bar", "hello", "world"}
		found, ok := Find(words, func(s string) bool { return len(s) > 4 })
		assert.True(t, ok)
		assert.Equal(t, "hello", found)
	})
}

// TestStringPipe tests the StringPipe convenience function
func TestStringPipe(t *testing.T) {
	t.Run("creates string transformation pipeline", func(t *testing.T) {
		slugify := StringPipe(
			strings.TrimSpace,
			strings.ToLower,
			StringReplace(" ", "-"),
			StringReplace("_", "-"),
		)

		result := slugify("  Hello_World Test  ")
		assert.Equal(t, "hello-world-test", result)
	})
}

// BenchmarkChainGeneric benchmarks the ChainGeneric function
func BenchmarkChainGeneric(b *testing.B) {
	h1 := func(x int) error { return nil }
	h2 := func(x int) error { return nil }
	h3 := func(x int) error { return nil }
	handler := ChainGeneric(h1, h2, h3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler(42)
	}
}

// BenchmarkPipe benchmarks the Pipe function
func BenchmarkPipe(b *testing.B) {
	pipe := Pipe(
		func(x int) int { return x + 1 },
		func(x int) int { return x * 2 },
		func(x int) int { return x - 1 },
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pipe(42)
	}
}
