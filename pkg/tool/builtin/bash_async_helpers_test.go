package toolbuiltin

import (
	"strings"
	"testing"
)

func TestParseAsyncFlag(t *testing.T) {
	cases := []struct {
		name    string
		params  map[string]interface{}
		want    bool
		wantErr string
	}{
		{name: "nil params", params: nil, want: false},
		{name: "missing async", params: map[string]interface{}{}, want: false},
		{name: "nil async", params: map[string]interface{}{"async": nil}, want: false},
		{name: "bool true", params: map[string]interface{}{"async": true}, want: true},
		{name: "bool false", params: map[string]interface{}{"async": false}, want: false},
		{name: "string true", params: map[string]interface{}{"async": "true"}, want: true},
		{name: "string false with spaces", params: map[string]interface{}{"async": " false "}, want: false},
		{name: "empty string", params: map[string]interface{}{"async": ""}, want: false},
		{name: "invalid string", params: map[string]interface{}{"async": "nope"}, wantErr: "async must be boolean"},
		{name: "invalid type", params: map[string]interface{}{"async": 123}, wantErr: "async must be boolean got"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAsyncFlag(tc.params)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestOptionalAsyncTaskID(t *testing.T) {
	cases := []struct {
		name    string
		params  map[string]interface{}
		want    string
		wantErr string
	}{
		{name: "nil params", params: nil, want: ""},
		{name: "missing task_id", params: map[string]interface{}{}, want: ""},
		{name: "nil task_id", params: map[string]interface{}{"task_id": nil}, want: ""},
		{name: "valid task_id", params: map[string]interface{}{"task_id": "t-1"}, want: "t-1"},
		{name: "empty task_id", params: map[string]interface{}{"task_id": "   "}, wantErr: "task_id cannot be empty"},
		{name: "non-string task_id", params: map[string]interface{}{"task_id": 123}, wantErr: "task_id must be string"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := optionalAsyncTaskID(tc.params)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
