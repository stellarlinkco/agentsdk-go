package api

import "strings"

// AvailableCommand describes a slash command that can be surfaced to ACP clients.
type AvailableCommand struct {
	Name        string
	Description string
}

// AvailableCommands returns the currently registered slash command definitions.
// The list is sorted by command priority/name in registration order semantics.
func (rt *Runtime) AvailableCommands() []AvailableCommand {
	if rt == nil || rt.cmdExec == nil {
		return nil
	}

	defs := rt.cmdExec.List()
	if len(defs) == 0 {
		return nil
	}

	out := make([]AvailableCommand, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		out = append(out, AvailableCommand{
			Name:        name,
			Description: strings.TrimSpace(def.Description),
		})
	}
	return out
}
