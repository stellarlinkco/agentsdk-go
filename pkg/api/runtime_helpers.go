package api

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cexll/agentsdk-go/pkg/message"
	"github.com/cexll/agentsdk-go/pkg/model"
	"github.com/cexll/agentsdk-go/pkg/runtime/commands"
	"github.com/cexll/agentsdk-go/pkg/runtime/skills"
	"github.com/cexll/agentsdk-go/pkg/runtime/subagents"
	"github.com/cexll/agentsdk-go/pkg/sandbox"
	"github.com/cexll/agentsdk-go/pkg/tool"
)

func availableTools(registry *tool.Registry, whitelist map[string]struct{}) []model.ToolDefinition {
	if registry == nil {
		return nil
	}
	tools := registry.List()
	defs := make([]model.ToolDefinition, 0, len(tools))
	for _, impl := range tools {
		if impl == nil {
			continue
		}
		name := strings.TrimSpace(impl.Name())
		if name == "" {
			continue
		}
		canon := canonicalToolName(name)
		if canon == "" {
			continue
		}
		if len(whitelist) > 0 {
			if _, ok := whitelist[canon]; !ok {
				continue
			}
		}
		defs = append(defs, model.ToolDefinition{
			Name:        name,
			Description: strings.TrimSpace(impl.Description()),
			Parameters:  schemaToMap(impl.Schema()),
		})
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

func schemaToMap(schema *tool.JSONSchema) map[string]any {
	if schema == nil {
		return nil
	}
	payload := map[string]any{}
	if schema.Type != "" {
		payload["type"] = schema.Type
	}
	if len(schema.Properties) > 0 {
		payload["properties"] = schema.Properties
	}
	if len(schema.Required) > 0 {
		payload["required"] = append([]string(nil), schema.Required...)
	}
	return payload
}

func convertMessages(msgs []message.Message) []model.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]model.Message, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, model.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			ToolCalls: convertToolCalls(msg.ToolCalls),
		})
	}
	return out
}

func convertToolCalls(calls []message.ToolCall) []model.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]model.ToolCall, len(calls))
	for i, call := range calls {
		out[i] = model.ToolCall{ID: call.ID, Name: call.Name, Arguments: cloneArguments(call.Arguments)}
	}
	return out
}

func cloneArguments(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	dup := make(map[string]any, len(args))
	for k, v := range args {
		dup[k] = v
	}
	return dup
}

func registerSkills(registrations []SkillRegistration) (*skills.Registry, error) {
	reg := skills.NewRegistry()
	for _, entry := range registrations {
		if entry.Handler == nil {
			return nil, errors.New("api: skill handler is nil")
		}
		if err := reg.Register(entry.Definition, entry.Handler); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

func registerCommands(registrations []CommandRegistration) (*commands.Executor, error) {
	exec := commands.NewExecutor()
	for _, entry := range registrations {
		if entry.Handler == nil {
			return nil, errors.New("api: command handler is nil")
		}
		if err := exec.Register(entry.Definition, entry.Handler); err != nil {
			return nil, err
		}
	}
	return exec, nil
}

func registerSubagents(registrations []SubagentRegistration) (*subagents.Manager, error) {
	if len(registrations) == 0 {
		return nil, nil
	}
	mgr := subagents.NewManager()
	for _, entry := range registrations {
		if entry.Handler == nil {
			return nil, errors.New("api: subagent handler is nil")
		}
		if err := mgr.Register(entry.Definition, entry.Handler); err != nil {
			return nil, err
		}
	}
	return mgr, nil
}

type historyStore struct {
	mu       sync.Mutex
	data     map[string]*message.History
	lastUsed map[string]time.Time
	maxSize  int
}

func newHistoryStore(maxSize int) *historyStore {
	if maxSize <= 0 {
		maxSize = defaultMaxSessions
	}
	return &historyStore{
		data:     map[string]*message.History{},
		lastUsed: map[string]time.Time{},
		maxSize:  maxSize,
	}
}

func (s *historyStore) Get(id string) *message.History {
	if strings.TrimSpace(id) == "" {
		id = defaultSessionID(defaultEntrypoint)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if hist, ok := s.data[id]; ok {
		s.lastUsed[id] = now
		return hist
	}
	hist := message.NewHistory()
	s.data[id] = hist
	s.lastUsed[id] = now
	if len(s.data) > s.maxSize {
		s.evictOldest()
	}
	return hist
}

func (s *historyStore) evictOldest() {
	if len(s.data) <= s.maxSize {
		return
	}
	var oldestKey string
	var oldestTime time.Time
	first := true
	for id, ts := range s.lastUsed {
		if first || ts.Before(oldestTime) {
			oldestKey = id
			oldestTime = ts
			first = false
		}
	}
	if oldestKey == "" {
		return
	}
	delete(s.data, oldestKey)
	delete(s.lastUsed, oldestKey)
}

func definitionSnapshot(exec *commands.Executor, name string) commands.Definition {
	if exec == nil {
		return commands.Definition{Name: strings.ToLower(name)}
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, def := range exec.List() {
		if def.Name == lower {
			return def
		}
	}
	return commands.Definition{Name: lower}
}

func snapshotSandbox(mgr *sandbox.Manager) SandboxReport {
	if mgr == nil {
		return SandboxReport{}
	}
	return SandboxReport{ResourceLimits: mgr.Limits()}
}
