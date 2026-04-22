package expr_test

import (
	"errors"
	"math"
	"testing"

	"github.com/KomeiDiSanXian/remilia/infra/expr"
)

func TestEval(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{"addition", "1+2", 3, false},
		{"subtraction", "10-3", 7, false},
		{"multiplication", "4*5", 20, false},
		{"division", "10/4", 2.5, false},
		{"parentheses", "(1+2)*3", 9, false},
		{"nested parens", "((2+3)*4)-5", 15, false},
		{"unary minus", "-5+10", 5, false},
		{"unary minus in parens", "-(3+2)", -5, false},
		{"24 point example", "(5+7)*2", 24, false},
		{"24 point fractions", "8/(3-1/3)", 3, false},
		{"spaces", "1 + 2 * 3", 7, false},
		{"fullwidth parens", "（1+2）*3", 9, false},
		{"multiplication sign", "3×4", 12, false},
		{"division sign", "12÷4", 3, false},
		{"division by zero", "1/0", 0, true},
		{"invalid char", "1+a", 0, true},
		{"missing close paren", "(1+2", 0, true},
		{"extra token", "1+2 3", 0, true},
		{"empty", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expr.Eval(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Eval(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("Eval(%q) unexpected error: %v", tt.input, err)
				return
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("Eval(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEvalWithNums(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantVal  float64
		wantNums []int
	}{
		{"simple", "1+2+3", 6, []int{1, 2, 3}},
		{"24 point", "(5+7)*2", 24, []int{5, 7, 2}},
		{"all four ops", "((8-4)*3)+2", 14, []int{8, 4, 3, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotNums, err := expr.EvalWithNums(tt.input)
			if err != nil {
				t.Fatalf("EvalWithNums(%q) unexpected error: %v", tt.input, err)
			}
			if math.Abs(gotVal-tt.wantVal) > 1e-9 {
				t.Errorf("val = %v, want %v", gotVal, tt.wantVal)
			}
			if len(gotNums) != len(tt.wantNums) {
				t.Fatalf("nums length = %d, want %d: %v vs %v", len(gotNums), len(tt.wantNums), gotNums, tt.wantNums)
			}
			for i := range gotNums {
				if gotNums[i] != tt.wantNums[i] {
					t.Errorf("nums[%d] = %d, want %d", i, gotNums[i], tt.wantNums[i])
				}
			}
		})
	}
}

func TestEvalErrors(t *testing.T) {
	t.Run("division by zero wraps ErrDivisionByZero", func(t *testing.T) {
		_, err := expr.Eval("5/0")
		if !errors.Is(err, expr.ErrDivisionByZero) {
			t.Errorf("expected ErrDivisionByZero, got %v", err)
		}
	})
	t.Run("invalid char wraps ErrInvalidExpr", func(t *testing.T) {
		_, err := expr.Eval("1+x")
		if !errors.Is(err, expr.ErrInvalidExpr) {
			t.Errorf("expected ErrInvalidExpr, got %v", err)
		}
	})
}
