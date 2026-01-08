package remilia

import "testing"

func TestEngineRuntime_InternalTypesUsed(t *testing.T) {
	var _ engineComponent = (*tempCleanerComponent)(nil)
	var _ engineComponent = (*pendingDeleteComponent)(nil)
	var _ = engineRuntime{}
	var _ = engineServices{}
}
