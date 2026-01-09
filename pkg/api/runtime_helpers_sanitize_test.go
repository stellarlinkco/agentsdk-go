package api

import "testing"

func TestSanitizePathComponent(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "default"},
		{in: "   ", want: "default"},
		{in: "sess-1", want: "sess-1"},
		{in: "a b", want: "a-b"},
		{in: "!!!", want: "default"},
		{in: "----", want: "default"},
		{in: "a/b", want: "a-b"},
	}

	for _, tc := range cases {
		if got := sanitizePathComponent(tc.in); got != tc.want {
			t.Fatalf("sanitizePathComponent(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}
