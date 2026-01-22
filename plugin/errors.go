package plugin

import "fmt"

// DependencyError 依赖错误
type DependencyError struct {
	Plugin     string
	Dependency string
	Err        error
}

func (e *DependencyError) Error() string {
	return fmt.Sprintf("plugin %s: dependency %s not found", e.Plugin, e.Dependency)
}

func (e *DependencyError) Unwrap() error {
	return e.Err
}

// CircularDependencyError 循环依赖错误
type CircularDependencyError struct {
	Cycle []string
}

func (e *CircularDependencyError) Error() string {
	return fmt.Sprintf("circular dependency detected: %v", e.Cycle)
}

func (e *CircularDependencyError) Unwrap() error {
	return ErrCircularDependency
}
