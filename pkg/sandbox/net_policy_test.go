package sandbox

import "testing"

func TestDomainAllowListValidate(t *testing.T) {
	policy := NewDomainAllowList("example.com", "*.svc.local")
	policy.Allow("EXAMPLE.com") // duplicate ignored

	if len(policy.Allowed()) != 2 {
		t.Fatalf("unexpected allowed snapshot: %v", policy.Allowed())
	}

	cases := []struct {
		host string
		ok   bool
	}{
		{"example.com", true},
		{"api.example.com", true},
		{"https://service.svc.local/query", true},
		{"SERVICE.SVC.LOCAL:443", true},
		{"other.com", false},
		{"", false},
	}

	for _, tc := range cases {
		err := policy.Validate(tc.host)
		if tc.ok && err != nil {
			t.Fatalf("expected %s allowed: %v", tc.host, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("expected %s denied", tc.host)
		}
	}

	if err := policy.Validate("   "); err == nil {
		t.Fatal("expected empty host rejection")
	}

	policy.Allow("")
	if len(policy.Allowed()) != 2 { // ignore empty allow
		t.Fatalf("empty host should be ignored, got %v", policy.Allowed())
	}
}
