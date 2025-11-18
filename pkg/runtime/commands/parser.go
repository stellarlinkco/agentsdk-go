package commands

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var (
	ErrNoCommand      = errors.New("commands: no slash command found")
	ErrInvalidCommand = errors.New("commands: invalid command")
)

// Invocation represents a parsed slash command invocation.
type Invocation struct {
	Name     string
	Args     []string
	Flags    map[string]string
	Raw      string
	Position int
}

// Flag retrieves a flag value.
func (i Invocation) Flag(name string) (string, bool) {
	if i.Flags == nil {
		return "", false
	}
	val, ok := i.Flags[strings.ToLower(name)]
	return val, ok
}

// Parse extracts slash commands from the input text. Each line beginning with
// '/' is treated as a command. Quoted arguments and --flag syntax are supported.
func Parse(input string) ([]Invocation, error) {
	lines := strings.Split(input, "\n")
	var invocations []Invocation
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
			continue
		}
		inv, err := parseLine(trimmed)
		if err != nil {
			return nil, fmt.Errorf("commands: line %d: %w", idx+1, err)
		}
		inv.Position = idx + 1
		inv.Raw = trimmed
		invocations = append(invocations, inv)
	}
	if len(invocations) == 0 {
		return nil, ErrNoCommand
	}
	return invocations, nil
}

func parseLine(line string) (Invocation, error) {
	tokens, err := lex(line)
	if err != nil {
		return Invocation{}, err
	}
	if len(tokens) == 0 {
		return Invocation{}, ErrInvalidCommand
	}
	name := tokens[0]
	if !strings.HasPrefix(name, "/") {
		return Invocation{}, ErrInvalidCommand
	}
	normalized := strings.ToLower(strings.TrimPrefix(name, "/"))
	if normalized == "" || !validName(normalized) {
		return Invocation{}, fmt.Errorf("commands: invalid name %q", name)
	}
	inv := Invocation{Name: normalized, Flags: map[string]string{}}
	for i := 1; i < len(tokens); i++ {
		token := tokens[i]
		if strings.HasPrefix(token, "--") {
			key, value, consumed := parseFlag(token)
			key = strings.ToLower(key)
			if key == "" {
				return Invocation{}, fmt.Errorf("commands: invalid flag %q", token)
			}
			if !consumed && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				value = tokens[i+1]
				i++
			}
			if value == "" {
				value = "true"
			}
			inv.Flags[key] = value
			continue
		}
		inv.Args = append(inv.Args, token)
	}
	if len(inv.Flags) == 0 {
		inv.Flags = nil
	}
	return inv, nil
}

func parseFlag(token string) (key, value string, hasValue bool) {
	trimmed := strings.TrimPrefix(token, "--")
	if idx := strings.Index(trimmed, "="); idx >= 0 {
		key = trimmed[:idx]
		value = trimmed[idx+1:]
		return strings.TrimSpace(key), value, true
	}
	return strings.TrimSpace(trimmed), "", false
}

func lex(line string) ([]string, error) {
	var tokens []string
	var buf strings.Builder
	var quote rune
	escaped := false
	emit := func() {
		if buf.Len() > 0 {
			tokens = append(tokens, buf.String())
			buf.Reset()
		}
	}
	for _, r := range line {
		switch {
		case escaped:
			buf.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			buf.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			emit()
		default:
			buf.WriteRune(r)
		}
	}
	if escaped {
		return nil, errors.New("commands: dangling escape")
	}
	if quote != 0 {
		return nil, errors.New("commands: unclosed quote")
	}
	emit()
	return tokens, nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
