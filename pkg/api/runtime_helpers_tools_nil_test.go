package api

import "testing"

func TestRuntimeAvailableToolsNilRuntime(t *testing.T) {
	var rt *Runtime
	if got := rt.AvailableTools(); got != nil {
		t.Fatalf("expected nil tools for nil runtime")
	}
	if got := rt.AvailableToolsForWhitelist([]string{"bash"}); got != nil {
		t.Fatalf("expected nil tools for nil runtime")
	}
}
