package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

var (
	// ErrEmptyCommand indicates the caller passed nothing to validate.
	ErrEmptyCommand = errors.New("security: empty command")
)

// Validator represents the second defensive ring: it blocks obviously dangerous intent.
type Validator struct {
	mu              sync.RWMutex
	bannedCommands  map[string]string
	bannedArguments []string
	bannedFragments []string
	maxCommandBytes int
	maxArgs         int
	// allowShellMeta permits |;&><`$ when true (for CLI scenarios)
	allowShellMeta bool
}

// NewValidator initialises the validator with conservative defaults.
func NewValidator() *Validator {
	return &Validator{
		bannedCommands: map[string]string{
			"dd":        "raw disk writes are unsafe",
			"mkfs":      "filesystem formatting is unsafe",
			"fdisk":     "partition editing is unsafe",
			"parted":    "partition editing is unsafe",
			"format":    "filesystem formatting is unsafe",
			"mkfs.ext4": "filesystem formatting is unsafe",
			"shutdown":  "system power management is forbidden",
			"reboot":    "system power management is forbidden",
			"halt":      "system power management is forbidden",
			"poweroff":  "system power management is forbidden",
			"mount":     "mount can expose host filesystem",
			"sudo":      "privilege escalation is forbidden",
		},
		bannedArguments: []string{
			"--no-preserve-root",
			"--preserve-root=false",
			"/dev/",
			"../",
		},
		// bannedFragments 捕捉危险的删除模式，而不是彻底禁止 rm/rmdir
		bannedFragments: []string{
			"-rf /",
			"--no-preserve-root",
			"--preserve-root=false",
			"rm -rf",
			"rm -fr",
			"rm -r",
			"rm --recursive",
			"rmdir -p",
			"rm *",
			"rm /",
		},
		maxCommandBytes: 32768,
		maxArgs:         512,
		allowShellMeta:  false,
	}
}

// SetMaxCommandBytes overrides the maximum allowed command length in bytes.
// Zero or negative disables the check.
func (v *Validator) SetMaxCommandBytes(n int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.maxCommandBytes = n
}

// SetMaxArgs overrides the maximum allowed argument count.
// Zero or negative disables the check.
func (v *Validator) SetMaxArgs(n int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.maxArgs = n
}

// AllowShellMetachars enables pipe and other shell features (CLI mode).
func (v *Validator) AllowShellMetachars(allow bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.allowShellMeta = allow
}

// Validate checks the provided command string.
func (v *Validator) Validate(input string) error {
	cmd := strings.TrimSpace(input)
	if cmd == "" {
		return ErrEmptyCommand
	}

	if v.maxCommandBytes > 0 && len(cmd) > v.maxCommandBytes {
		return fmt.Errorf("security: command too long (%d bytes)", len(cmd))
	}

	if containsControl(cmd) {
		return fmt.Errorf("security: control characters detected")
	}

	v.mu.RLock()
	allowMeta := v.allowShellMeta
	v.mu.RUnlock()

	if !allowMeta && strings.ContainsAny(cmd, "|;&><`$") {
		return fmt.Errorf("security: pipe or shell metacharacters are blocked")
	}

	args, err := splitCommand(cmd)
	if err != nil {
		return fmt.Errorf("security: parse failed: %w", err)
	}
	if len(args) == 0 {
		return ErrEmptyCommand
	}

	if v.maxArgs > 0 && len(args) > v.maxArgs {
		return fmt.Errorf("security: too many arguments (%d)", len(args))
	}

	base := filepath.Base(args[0])

	v.mu.RLock()
	reason, banned := v.bannedCommands[base]
	argRules := append([]string(nil), v.bannedArguments...)
	fragments := append([]string(nil), v.bannedFragments...)
	v.mu.RUnlock()

	if banned {
		return fmt.Errorf("security: %s (%s)", base, reason)
	}

	lowerCmd := strings.ToLower(cmd)
	for _, fragment := range fragments {
		if fragment == "" {
			continue
		}
		if strings.Contains(lowerCmd, strings.ToLower(fragment)) {
			return fmt.Errorf("security: command fragment %q is banned", fragment)
		}
	}

	for _, arg := range args[1:] {
		for _, bannedArg := range argRules {
			if strings.Contains(strings.ToLower(arg), strings.ToLower(bannedArg)) {
				return fmt.Errorf("security: argument %q is banned", arg)
			}
		}
	}

	return nil
}

// containsControl reports if the string contains control characters except tab/space/newline.
func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}

// splitCommand tokenises a simple shell command with quote awareness.
func splitCommand(input string) ([]string, error) {
	var (
		args               []string
		current            strings.Builder
		inSingle, inDouble bool
		escape             bool
	)

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range input {
		switch {
		case escape:
			current.WriteRune(r)
			escape = false
		case r == '\\':
			if inSingle {
				current.WriteRune(r)
				continue
			}
			escape = true
		case r == '\'':
			if inDouble {
				current.WriteRune(r)
				continue
			}
			if inSingle {
				inSingle = false
				continue
			}
			inSingle = true
		case r == '"':
			if inSingle {
				current.WriteRune(r)
				continue
			}
			if inDouble {
				inDouble = false
				continue
			}
			inDouble = true
		case unicode.IsSpace(r):
			if inSingle || inDouble {
				current.WriteRune(r)
			} else {
				flush()
			}
		default:
			current.WriteRune(r)
		}
	}

	if escape {
		return nil, fmt.Errorf("unfinished escape sequence")
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return args, nil
}
