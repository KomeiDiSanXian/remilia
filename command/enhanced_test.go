package command

import "testing"

func TestEnhancedAPI_Compiles(t *testing.T) {
	p := NewParser("/")
	p.Register(&Definition{Name: "echo"})
	_, _ = ParseFromDefinition("/echo", &Definition{Name: "echo"}, "/")
}
