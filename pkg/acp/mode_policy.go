package acp

import "strings"

var architectReadOnlyToolList = []string{
	"read",
	"glob",
	"grep",
	"webfetch",
	"websearch",
	"bashoutput",
	"bashstatus",
	"taskget",
	"tasklist",
	"askuserquestion",
}

var architectReadOnlyToolSet = map[string]struct{}{
	"read":            {},
	"glob":            {},
	"grep":            {},
	"webfetch":        {},
	"websearch":       {},
	"bashoutput":      {},
	"bashstatus":      {},
	"taskget":         {},
	"tasklist":        {},
	"askuserquestion": {},
}

func architectToolWhitelist() []string {
	return append([]string(nil), architectReadOnlyToolList...)
}

func isArchitectReadOnlyTool(name string) bool {
	_, ok := architectReadOnlyToolSet[canonicalACPToolName(name)]
	return ok
}

func canonicalACPToolName(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(key)
}
