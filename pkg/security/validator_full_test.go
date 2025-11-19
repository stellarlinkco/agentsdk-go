package security

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidatorBlocksBannedCommands(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "rm -rf / blocked by fragment", cmd: "rm -rf /", want: "fragment"},
		{name: "mkfs command", cmd: "mkfs /dev/sda", want: "mkfs"},
		{name: "dd command", cmd: "dd if=/dev/zero of=/dev/null", want: "dd"},
		{name: "format command", cmd: "format disk", want: "format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator()
			err := v.Validate(tt.cmd)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q got %v", tt.want, err)
			}
		})
	}
}

func TestValidatorRejectsInjectionPatterns(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "pipe metacharacter", cmd: "ls | rm -rf /", want: "metacharacters"},
		{name: "redirection metacharacter", cmd: "cat secret > /tmp/out", want: "metacharacters"},
		{name: "command chaining", cmd: "echo ok && rm -rf /", want: "metacharacters"},
		{name: "semicolon attack", cmd: "echo ok; rm -rf /", want: "metacharacters"},
		{name: "subshell expansion", cmd: "echo $(rm -rf /)", want: "metacharacters"},
		{name: "banned fragment", cmd: "touch --no-preserve-root", want: "fragment"},
		{name: "parent traversal argument", cmd: "cat ../etc/passwd", want: "argument"},
		{name: "/dev argument", cmd: "cp file /dev/sda", want: "argument"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator()
			err := v.Validate(tt.cmd)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q got %v", tt.want, err)
			}
		})
	}
}

func TestValidatorEdgeCasesAndLimits(t *testing.T) {
	tests := []struct {
		name  string
		cmd   string
		tweak func(v *Validator)
		want  string
	}{
		{name: "empty command", cmd: "   ", want: ErrEmptyCommand.Error()},
		{
			name: "control characters",
			cmd:  "echo hi" + string(rune(0)),
			want: "control characters",
		},
		{
			name: "unterminated quote",
			cmd:  "echo \"unterminated",
			want: "parse failed",
		},
		{
			name: "too many args",
			cmd:  "printf one two",
			tweak: func(v *Validator) {
				v.mu.Lock()
				defer v.mu.Unlock()
				v.maxArgs = 1
			},
			want: "too many arguments",
		},
		{
			name: "command too long",
			cmd:  strings.Repeat("a", 10),
			tweak: func(v *Validator) {
				v.mu.Lock()
				defer v.mu.Unlock()
				v.maxCommandBytes = 5
			},
			want: "command too long",
		},
		{name: "safe command allowed", cmd: "printf \"hello world\"", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator()
			if tt.tweak != nil {
				tt.tweak(v)
			}
			err := v.Validate(tt.cmd)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q got %v", tt.want, err)
			}
		})
	}
}

func TestSplitCommandHandlesQuotesAndEscapes(t *testing.T) {
	cmd := `echo "hello world" 'and more' arg\ with\ spaces`
	args, err := splitCommand(cmd)
	if err != nil {
		t.Fatalf("splitCommand: %v", err)
	}
	want := []string{"echo", "hello world", "and more", "arg with spaces"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestSplitCommandDetectsEdgeErrors(t *testing.T) {
	if _, err := splitCommand(`printf unfinished\`); err == nil || !strings.Contains(err.Error(), "unfinished escape") {
		t.Fatalf("expected unfinished escape error got %v", err)
	}
	if _, err := splitCommand(`echo "missing end`); err == nil || !strings.Contains(err.Error(), "unterminated quote") {
		t.Fatalf("expected quote error got %v", err)
	}
}
