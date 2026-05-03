package lifecycle

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestStartError_Error(t *testing.T) {
	tests := []struct {
		name      string
		component string
		phase     string
		err       error
		want      string
	}{
		{
			name:      "basic error",
			component: "http-server",
			phase:     "listen",
			err:       errors.New("port 8080 in use"),
			want:      "Lifecycle: component 'http-server' listen failed: port 8080 in use",
		},
		{
			name:      "empty component",
			component: "",
			phase:     "init",
			err:       errors.New("something went wrong"),
			want:      "Lifecycle: component '' init failed: something went wrong",
		},
		{
			name:      "nil underlying error",
			component: "db",
			phase:     "connect",
			err:       nil,
			want:      "Lifecycle: component 'db' connect failed: <nil>",
		},
		{
			name:      "wrapped sentinel",
			component: "cache",
			phase:     "open",
			err:       fmt.Errorf("wrapped: %w", io.EOF),
			want:      "Lifecycle: component 'cache' open failed: wrapped: EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &StartError{
				Component: tt.component,
				Phase:     tt.phase,
				Err:       tt.err,
			}
			if got := e.Error(); got != tt.want {
				t.Errorf("StartError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStartError_Unwrap(t *testing.T) {
	t.Run("returns wrapped error", func(t *testing.T) {
		inner := errors.New("inner error")
		e := &StartError{Component: "x", Phase: "y", Err: inner}
		if got := e.Unwrap(); !errors.Is(got, inner) {
			t.Errorf("StartError.Unwrap() = %v, want %v", got, inner)
		}
	})

	t.Run("returns nil", func(t *testing.T) {
		e := &StartError{Component: "x", Phase: "y", Err: nil}
		if got := e.Unwrap(); got != nil {
			t.Errorf("StartError.Unwrap() = %v, want nil", got)
		}
	})
}

func TestStartError_ErrorIs(t *testing.T) {
	inner := errors.New("inner error")
	outer := &StartError{Component: "x", Phase: "y", Err: inner}

	if !errors.Is(outer, inner) {
		t.Error("errors.Is(outer, inner) should be true")
	}

	if errors.Is(outer, io.EOF) {
		t.Error("errors.Is(outer, io.EOF) should be false")
	}
}

func TestStartError_As(t *testing.T) {
	inner := errors.New("inner error")
	outer := &StartError{Component: "x", Phase: "y", Err: inner}

	var target *StartError
	if !errors.As(outer, &target) {
		t.Fatal("errors.As should match *StartError")
	}
	if target.Component != "x" || target.Phase != "y" || !errors.Is(inner, target.Err) {
		t.Error("target fields mismatch")
	}
}

func TestStopError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "basic error",
			err:  errors.New("connection reset"),
			want: "Lifecycle: stop failed: connection reset",
		},
		{
			name: "nil underlying error",
			err:  nil,
			want: "Lifecycle: stop failed: <nil>",
		},
		{
			name: "sentinel error",
			err:  io.EOF,
			want: "Lifecycle: stop failed: EOF",
		},
		{
			name: "wrapped error",
			err:  fmt.Errorf("nested: %w", io.ErrUnexpectedEOF),
			want: "Lifecycle: stop failed: nested: unexpected EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &StopError{Err: tt.err}
			if got := e.Error(); got != tt.want {
				t.Errorf("StopError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStopError_Unwrap(t *testing.T) {
	t.Run("returns wrapped error", func(t *testing.T) {
		inner := errors.New("inner error")
		e := &StopError{Err: inner}
		if got := e.Unwrap(); !errors.Is(got, inner) {
			t.Errorf("StopError.Unwrap() = %v, want %v", got, inner)
		}
	})

	t.Run("returns nil", func(t *testing.T) {
		e := &StopError{Err: nil}
		if got := e.Unwrap(); got != nil {
			t.Errorf("StopError.Unwrap() = %v, want nil", got)
		}
	})
}

func TestStopError_ErrorIs(t *testing.T) {
	outer := &StopError{Err: io.EOF}

	if !errors.Is(outer, io.EOF) {
		t.Error("errors.Is(outer, io.EOF) should be true")
	}

	if errors.Is(outer, io.ErrUnexpectedEOF) {
		t.Error("errors.Is(outer, io.ErrUnexpectedEOF) should be false")
	}
}

func TestStopError_As(t *testing.T) {
	outer := &StopError{Err: io.EOF}

	var target *StopError
	if !errors.As(outer, &target) {
		t.Fatal("errors.As should match *StopError")
	}
	if target.Err != io.EOF {
		t.Error("target.Err mismatch")
	}
}

func TestErrInvalidState_Error(t *testing.T) {
	tests := []struct {
		name     string
		current  State
		expected State
		want     string
	}{
		{
			name:     "created to running",
			current:  StateCreated,
			expected: StateRunning,
			want:     "Lifecycle: invalid state: current=created, expected=running",
		},
		{
			name:     "running to starting",
			current:  StateRunning,
			expected: StateStarting,
			want:     "Lifecycle: invalid state: current=running, expected=starting",
		},
		{
			name:     "stopped to stopping",
			current:  StateStopped,
			expected: StateStopping,
			want:     "Lifecycle: invalid state: current=stopped, expected=stopping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := ErrInvalidState{Current: tt.current, Expected: tt.expected}
			if got := e.Error(); got != tt.want {
				t.Errorf("ErrInvalidState.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrInvalidState_ValueSemantics(t *testing.T) {
	original := ErrInvalidState{Current: StateCreated, Expected: StateRunning}
	copied := original

	copied.Current = StateStarting
	copied.Expected = StateStopped

	if original.Current != StateCreated {
		t.Error("modifying copy changed original.Current")
	}
	if original.Expected != StateRunning {
		t.Error("modifying copy changed original.Expected")
	}
}

func TestErrInvalidState_ImplementsError(t *testing.T) {
	var err error = ErrInvalidState{Current: StateCreated, Expected: StateRunning}
	if err == nil {
		t.Fatal("ErrInvalidState should be non-nil when assigned to error interface")
	}
	want := "Lifecycle: invalid state: current=created, expected=running"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestErrInvalidState_AllStates(t *testing.T) {
	states := []State{StateCreated, StateStarting, StateRunning, StateStopping, StateStopped}

	for _, current := range states {
		for _, expected := range states {
			if current == expected {
				continue
			}
			t.Run(fmt.Sprintf("%s_to_%s", current, expected), func(t *testing.T) {
				e := ErrInvalidState{Current: current, Expected: expected}
				got := e.Error()
				want := fmt.Sprintf("Lifecycle: invalid state: current=%s, expected=%s", current, expected)
				if got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			})
		}
	}
}
