package command

import "testing"

func TestEnhancedAPI_Compiles(t *testing.T) {
	p := NewCommandParser("/")
	p.Register(&CommandDefinition{Name: "echo"})
	_, _ = ParseFromDefinition("/echo", &CommandDefinition{Name: "echo"}, "/")
}
