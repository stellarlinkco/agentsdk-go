package sandbox

import (
	"errors"
	"testing"
)

func TestDomainAllowList_NilReceiver(t *testing.T) {
	var policy *DomainAllowList

	// Must not panic.
	policy.Allow("example.com")
	if policy.Allowed() != nil {
		t.Fatalf("expected nil Allowed() on nil receiver")
	}
	if err := policy.Validate("example.com"); err == nil || !errors.Is(err, ErrDomainDenied) {
		t.Fatalf("expected ErrDomainDenied on nil receiver, got %v", err)
	}
}

func TestDomainAllowList_NormalizeHostAndMatchesHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "HTTP://Example.COM:443", want: "example.com"},
		{in: "[::1]:443", want: "::1"},
		{in: "[::1]", want: "::1"},
		{in: ".Example.com", want: "example.com"},
	}
	for _, tc := range cases {
		if got := normalizeHost(tc.in); got != tc.want {
			t.Fatalf("normalizeHost(%q)=%q want %q", tc.in, got, tc.want)
		}
	}

	if matchesHost("a", "") {
		t.Fatalf("expected empty allowlist entry to not match")
	}
	if !matchesHost("example.com", "example.com") {
		t.Fatalf("expected exact match")
	}
	if !matchesHost("api.example.com", "*.example.com") {
		t.Fatalf("expected wildcard match")
	}
	if !matchesHost("api.example.com", "example.com") {
		t.Fatalf("expected suffix match")
	}
}
