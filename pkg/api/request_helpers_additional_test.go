package api

import "testing"

func TestCombineToolWhitelistsIntersection(t *testing.T) {
	result := combineToolWhitelists([]string{"A", "B"}, []string{"b", "c"})
	if len(result) != 1 {
		t.Fatalf("expected single intersection, got %+v", result)
	}
	if _, ok := result["b"]; !ok {
		t.Fatalf("missing expected intersection result: %+v", result)
	}
}

func TestCombineToolWhitelistsNilInputs(t *testing.T) {
	if res := combineToolWhitelists(nil, nil); res != nil {
		t.Fatalf("expected nil for empty inputs, got %+v", res)
	}
}
