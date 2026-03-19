package subagents

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	"gopkg.in/yaml.v3"
)

// LoaderOptions controls how subagents are discovered from the filesystem.
type LoaderOptions struct {
	ProjectRoot string
	// Deprecated: user-level scanning has been removed; this field is ignored.
	UserHome string
	// Deprecated: user-level scanning has been removed; this flag is ignored.
	EnableUser bool
	FS         *config.FS
}

// SubagentFile captures an on-disk subagent definition.
type SubagentFile struct {
	Name     string
	Path     string
	Metadata SubagentMetadata
	Body     string
}

// SubagentMetadata mirrors the YAML frontmatter fields.
type SubagentMetadata struct {
	Name           string `yaml:"name"`
	Description    string `yaml:"description"`
	Tools          string `yaml:"tools"`          // comma separated
	Model          string `yaml:"model"`          // sonnet/opus/haiku/inherit
	PermissionMode string `yaml:"permissionMode"` // default/acceptEdits/bypassPermissions/plan/ignore
	Skills         string `yaml:"skills"`         // comma separated
}

// SubagentRegistration wires a definition to its handler.
type SubagentRegistration struct {
	Definition Definition
	Handler    Handler
}

var (
	subagentNameRegexp = regexp.MustCompile(`^[a-z0-9-]+$`)
	allowedModels      = map[string]struct{}{"sonnet": {}, "opus": {}, "haiku": {}, "inherit": {}}
	allowedPermission  = map[string]struct{}{
		"default":           {},
		"acceptedits":       {},
		"bypasspermissions": {},
		"plan":              {},
		"ignore":            {},
	}
)

// LoadFromFS loads subagent definitions. Errors are aggregated so a single bad
// file will not block other registrations.
func LoadFromFS(opts LoaderOptions) ([]SubagentRegistration, []error) {
	var (
		registrations []SubagentRegistration
		errs          []error
		merged        = map[string]SubagentFile{}
	)

	fsLayer := opts.FS
	if fsLayer == nil {
		fsLayer = config.NewFS(opts.ProjectRoot, nil)
	}

	agentsDir := filepath.Join(opts.ProjectRoot, ".agents", "agents")
	files, loadErrs := loadSubagentDir(agentsDir, fsLayer)
	errs = append(errs, loadErrs...)
	for name, file := range files {
		merged[name] = file
	}

	if len(merged) == 0 {
		return nil, errs
	}

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		file := merged[name]
		model := normalizeModel(file.Metadata.Model)
		whitelist := parseList(file.Metadata.Tools)
		skills := parseList(file.Metadata.Skills)
		meta := buildMetadataMap(file, whitelist, skills, model)

		def := Definition{
			Name:         file.Metadata.Name,
			Description:  file.Metadata.Description,
			BaseContext:  Context{ToolWhitelist: whitelist, Model: model, Metadata: meta},
			DefaultModel: model,
		}

		reg := SubagentRegistration{
			Definition: def,
			Handler:    buildHandler(file, meta),
		}
		registrations = append(registrations, reg)
	}

	return registrations, errs
}

func loadSubagentDir(root string, fsLayer *config.FS) (map[string]SubagentFile, []error) {
	results := map[string]SubagentFile{}
	var errs []error

	if fsLayer == nil {
		fsLayer = config.NewFS("", nil)
	}

	info, err := fsLayer.Stat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return results, nil
		}
		return results, []error{fmt.Errorf("subagents: stat %s: %w", root, err)}
	}
	if !info.IsDir() {
		return results, []error{fmt.Errorf("subagents: path %s is not a directory", root)}
	}

	walkErr := fsLayer.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			errs = append(errs, fmt.Errorf("subagents: walk %s: %w", path, walkErr))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}

		fallback := strings.ToLower(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
		file, parseErr := parseSubagentFile(path, fallback, fsLayer)
		if parseErr != nil {
			errs = append(errs, parseErr)
			return nil
		}
		if _, exists := results[file.Metadata.Name]; exists {
			errs = append(errs, fmt.Errorf("subagents: duplicate subagent %q in %s", file.Metadata.Name, root))
			return nil
		}
		results[file.Metadata.Name] = file
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	return results, errs
}

func parseSubagentFile(path, fallback string, fsLayer *config.FS) (SubagentFile, error) {
	if fsLayer == nil {
		fsLayer = config.NewFS("", nil)
	}

	data, err := fsLayer.ReadFile(path)
	if err != nil {
		return SubagentFile{}, fmt.Errorf("subagents: read %s: %w", path, err)
	}
	meta, body, err := parseFrontMatter(string(data))
	if err != nil {
		return SubagentFile{}, fmt.Errorf("subagents: parse %s: %w", path, err)
	}

	meta.Name = strings.ToLower(strings.TrimSpace(meta.Name))
	meta.Description = strings.TrimSpace(meta.Description)
	meta.Tools = strings.TrimSpace(meta.Tools)
	meta.Model = strings.ToLower(strings.TrimSpace(meta.Model))
	meta.PermissionMode = strings.TrimSpace(meta.PermissionMode)
	meta.Skills = strings.TrimSpace(meta.Skills)

	if meta.Name == "" {
		meta.Name = strings.ToLower(strings.TrimSpace(fallback))
	}
	if err := validateMetadata(meta); err != nil {
		return SubagentFile{}, fmt.Errorf("subagents: validate %s: %w", path, err)
	}

	return SubagentFile{
		Name:     meta.Name,
		Path:     path,
		Metadata: meta,
		Body:     body,
	}, nil
}

func parseFrontMatter(content string) (SubagentMetadata, string, error) {
	trimmed := strings.TrimPrefix(content, "\uFEFF") // drop BOM if present
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return SubagentMetadata{}, "", errors.New("missing YAML frontmatter")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return SubagentMetadata{}, "", errors.New("missing closing frontmatter separator")
	}

	metaText := strings.Join(lines[1:end], "\n")
	var meta SubagentMetadata
	if err := yaml.Unmarshal([]byte(metaText), &meta); err != nil {
		return SubagentMetadata{}, "", fmt.Errorf("decode YAML: %w", err)
	}

	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimPrefix(body, "\n")
	return meta, body, nil
}

func validateMetadata(meta SubagentMetadata) error {
	if meta.Name == "" {
		return errors.New("name is required")
	}
	if !subagentNameRegexp.MatchString(meta.Name) {
		return fmt.Errorf("invalid name %q", meta.Name)
	}
	desc := strings.TrimSpace(meta.Description)
	if desc == "" {
		return errors.New("description is required")
	}
	if meta.Model != "" {
		if _, ok := allowedModels[meta.Model]; !ok {
			return fmt.Errorf("invalid model %q", meta.Model)
		}
	}
	if pm := strings.ToLower(meta.PermissionMode); pm != "" {
		if _, ok := allowedPermission[pm]; !ok {
			return fmt.Errorf("invalid permissionMode %q", meta.PermissionMode)
		}
	}
	return nil
}

func normalizeModel(model string) string {
	if model == "" || model == "inherit" {
		return ""
	}
	if _, ok := allowedModels[model]; ok {
		return model
	}
	return ""
}

func parseList(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := map[string]struct{}{}
	var out []string
	for _, part := range parts {
		val := strings.ToLower(strings.TrimSpace(part))
		if val == "" {
			continue
		}
		if _, ok := seen[val]; ok {
			continue
		}
		seen[val] = struct{}{}
		out = append(out, val)
	}
	sort.Strings(out)
	return out
}

func buildMetadataMap(file SubagentFile, tools, skills []string, model string) map[string]any {
	meta := map[string]any{}
	if len(tools) > 0 {
		meta["tools"] = tools
	}
	if model != "" {
		meta["model"] = model
	}
	if pm := strings.ToLower(strings.TrimSpace(file.Metadata.PermissionMode)); pm != "" {
		meta["permission-mode"] = pm
	}
	if len(skills) > 0 {
		meta["skills"] = skills
	}
	if file.Path != "" {
		meta["source"] = file.Path
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func buildHandler(file SubagentFile, meta map[string]any) Handler {
	body := file.Body
	return HandlerFunc(func(context.Context, Context, Request) (Result, error) {
		res := Result{Output: body}
		if len(meta) > 0 {
			res.Metadata = meta
		}
		return res, nil
	})
}
