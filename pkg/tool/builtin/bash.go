package toolbuiltin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/stellarlinkco/agentsdk-go/pkg/middleware"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/sandbox"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

const (
	defaultBashTimeout = 10 * time.Minute
	maxBashTimeout     = 60 * time.Minute
	maxBashOutputLen   = 30000
	bashDescript       = `
	Execute a bash command with a configurable timeout.
	Prefer dedicated file tools (read/write/edit/glob/grep) over shell pipelines.
	`
)

var (
	bashGetwd       = os.Getwd
	bashFilepathAbs = filepath.Abs

	bashFileClose = func(f *os.File) error { return f.Close() }
	bashFileSeek  = func(f *os.File, offset int64, whence int) (int64, error) { return f.Seek(offset, whence) }

	bashRandRead = rand.Read
)

var bashSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"command": map[string]interface{}{
			"type":        "string",
			"description": "Command string to execute. Shell metacharacters are rejected by default unless explicitly enabled by the runtime.",
		},
		"timeout": map[string]interface{}{
			"type":        "number",
			"description": "Optional timeout in seconds (defaults to 600, caps at 3600).",
		},
		"workdir": map[string]interface{}{
			"type":        "string",
			"description": "Optional working directory relative to the sandbox root.",
		},
	},
	Required: []string{"command"},
}

// BashTool executes validated commands using bash within a sandbox.
type BashTool struct {
	policy    sandbox.FileSystemPolicy
	validator *bashCommandValidator

	root    string
	timeout time.Duration

	outputThresholdBytes int
	openPipes            func(*exec.Cmd) (io.ReadCloser, io.ReadCloser, error)
}

// NewBashTool builds a BashTool rooted at the current directory.
func NewBashTool() *BashTool {
	return NewBashToolWithRoot("")
}

// NewBashToolWithRoot builds a BashTool rooted at the provided directory.
func NewBashToolWithRoot(root string) *BashTool {
	resolved := resolveRoot(root)
	return &BashTool{
		policy:    sandbox.NewFileSystemAllowList(resolved),
		validator: newBashCommandValidator(),

		root:    resolved,
		timeout: defaultBashTimeout,

		outputThresholdBytes: maxBashOutputLen,
	}
}

// NewBashToolWithSandbox builds a BashTool with a custom sandbox.
// Used when sandbox needs to be pre-configured (e.g., disabled mode).
func NewBashToolWithSandbox(root string, policy sandbox.FileSystemPolicy) *BashTool {
	resolved := resolveRoot(root)
	return &BashTool{
		policy:    policy,
		validator: newBashCommandValidator(),

		root:    resolved,
		timeout: defaultBashTimeout,

		outputThresholdBytes: maxBashOutputLen,
	}
}

// SetOutputThresholdBytes controls when output is spooled to disk.
func (b *BashTool) SetOutputThresholdBytes(threshold int) {
	if b == nil {
		return
	}
	b.outputThresholdBytes = threshold
}

func (b *BashTool) effectiveOutputThresholdBytes() int {
	if b == nil || b.outputThresholdBytes <= 0 {
		return maxBashOutputLen
	}
	return b.outputThresholdBytes
}

// SetCommandLimits overrides the maximum command length (bytes) and argument count
// enforced by the security validator. Use this for code-generation scenarios where
// agents write files via bash heredocs or long cat commands.
func (b *BashTool) SetCommandLimits(maxBytes, maxArgs int) {
	if b != nil && b.validator != nil {
		b.validator.SetMaxCommandBytes(maxBytes)
		b.validator.SetMaxArgs(maxArgs)
	}
}

// AllowShellMetachars enables shell pipes and metacharacters (CLI mode).
func (b *BashTool) AllowShellMetachars(allow bool) {
	if b != nil && b.validator != nil {
		b.validator.AllowShellMetachars(allow)
	}
}

func (b *BashTool) Name() string { return "bash" }

func (b *BashTool) Description() string {
	return bashDescript
}

func (b *BashTool) Schema() *tool.JSONSchema { return bashSchema }

func (b *BashTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if b == nil || b.validator == nil {
		return nil, errors.New("bash tool is not initialised")
	}
	command, err := extractCommand(params)
	if err != nil {
		return nil, err
	}
	if err := b.validator.Validate(command); err != nil {
		return nil, err
	}
	workdir, err := b.resolveWorkdir(params)
	if err != nil {
		return nil, err
	}
	timeout, err := b.resolveTimeout(params)
	if err != nil {
		return nil, err
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, "bash", "-c", command)
	cmd.Env = os.Environ()
	cmd.Dir = workdir

	spool := newBashOutputSpool(ctx, b.effectiveOutputThresholdBytes())
	cmd.Stdout = spool.StdoutWriter()
	cmd.Stderr = spool.StderrWriter()

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	output, outputFile, spoolErr := spool.Finalize()

	data := map[string]interface{}{
		"workdir":     workdir,
		"duration_ms": duration.Milliseconds(),
		"timeout_ms":  timeout.Milliseconds(),
	}
	if outputFile != "" {
		data["output_file"] = outputFile
	}
	if spoolErr != nil {
		data["spool_error"] = spoolErr.Error()
	}

	result := &tool.ToolResult{
		Success: runErr == nil,
		Output:  output,
		Data:    data,
	}

	if runErr != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return result, fmt.Errorf("command timeout after %s", timeout)
		}
		return result, fmt.Errorf("command failed: %w", runErr)
	}
	return result, nil
}

func (b *BashTool) resolveWorkdir(params map[string]interface{}) (string, error) {
	dir := b.root
	if raw, ok := params["workdir"]; ok && raw != nil {
		value, err := coerceString(raw)
		if err != nil {
			return "", fmt.Errorf("workdir must be string: %w", err)
		}
		value = strings.TrimSpace(value)
		if value != "" {
			dir = value
		}
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(b.root, dir)
	}
	dir = filepath.Clean(dir)
	return b.ensureDirectory(dir)
}

func (b *BashTool) ensureDirectory(path string) (string, error) {
	if b.policy != nil {
		if err := b.policy.Validate(path); err != nil {
			return "", err
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("workdir stat: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %s is not a directory", path)
	}
	return path, nil
}

func (b *BashTool) resolveTimeout(params map[string]interface{}) (time.Duration, error) {
	timeout := b.timeout
	raw, ok := params["timeout"]
	if !ok || raw == nil {
		return timeout, nil
	}
	dur, err := durationFromParam(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout: %w", err)
	}
	if dur == 0 {
		return timeout, nil
	}
	if dur > maxBashTimeout {
		dur = maxBashTimeout
	}
	return dur, nil
}

func extractCommand(params map[string]interface{}) (string, error) {
	if params == nil {
		return "", errors.New("params is nil")
	}
	raw, ok := params["command"]
	if !ok {
		// 提供更详细的错误信息帮助调试
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		if len(keys) == 0 {
			return "", errors.New("command is required (params is empty)")
		}
		return "", fmt.Errorf("command is required (got params with keys: %v)", keys)
	}
	cmd, err := coerceString(raw)
	if err != nil {
		return "", fmt.Errorf("command must be string: %w", err)
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", errors.New("command cannot be empty")
	}
	return cmd, nil
}

func coerceString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	case fmt.Stringer:
		return v.String(), nil
	case []byte:
		return string(v), nil
	default:
		return "", fmt.Errorf("expected string got %T", value)
	}
}

func durationFromParam(value interface{}) (time.Duration, error) {
	switch v := value.(type) {
	case time.Duration:
		if v < 0 {
			return 0, errors.New("duration cannot be negative")
		}
		return v, nil
	case float64:
		return secondsToDuration(v)
	case float32:
		return secondsToDuration(float64(v))
	case int:
		return secondsToDuration(float64(v))
	case int64:
		return secondsToDuration(float64(v))
	case uint:
		return secondsToDuration(float64(v))
	case uint64:
		return secondsToDuration(float64(v))
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, err
		}
		return secondsToDuration(f)
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, nil
		}
		if strings.ContainsAny(trimmed, "hms") {
			return time.ParseDuration(trimmed)
		}
		f, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, err
		}
		return secondsToDuration(f)
	default:
		return 0, fmt.Errorf("unsupported duration type %T", value)
	}
}

func secondsToDuration(seconds float64) (time.Duration, error) {
	if seconds < 0 {
		return 0, errors.New("duration cannot be negative")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func combineOutput(stdout, stderr string) string {
	stdout = strings.TrimRight(stdout, "\r\n")
	stderr = strings.TrimRight(stderr, "\r\n")
	switch {
	case stdout == "" && stderr == "":
		return ""
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	default:
		return stdout + "\n" + stderr
	}
}

func resolveRoot(dir string) string {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		if cwd, err := bashGetwd(); err == nil {
			trimmed = cwd
		} else {
			trimmed = "."
		}
	}
	if abs, err := bashFilepathAbs(trimmed); err == nil {
		return abs
	}
	return filepath.Clean(trimmed)
}

type bashOutputSpool struct {
	threshold  int
	outputPath string
	stdout     *tool.SpoolWriter
	stderr     *tool.SpoolWriter
}

func newBashOutputSpool(ctx context.Context, threshold int) *bashOutputSpool {
	sessionID := bashSessionID(ctx)
	dir := filepath.Join(bashOutputBaseDir(), sanitizePathComponent(sessionID))
	filename := bashOutputFilename()
	outputPath := filepath.Join(dir, filename)

	spool := &bashOutputSpool{
		threshold:  threshold,
		outputPath: outputPath,
	}
	spool.stdout = tool.NewSpoolWriter(threshold, func() (io.WriteCloser, string, error) {
		return openBashOutputFile(outputPath)
	})
	spool.stderr = tool.NewSpoolWriter(threshold, func() (io.WriteCloser, string, error) {
		if err := ensureBashOutputDir(dir); err != nil {
			return nil, "", err
		}
		f, err := os.CreateTemp(dir, "stderr-*.tmp")
		if err != nil {
			return nil, "", err
		}
		return f, f.Name(), nil
	})
	return spool
}

func (s *bashOutputSpool) StdoutWriter() io.Writer { return s.stdout }

func (s *bashOutputSpool) StderrWriter() io.Writer { return s.stderr }

func (s *bashOutputSpool) Append(text string, isStderr bool) error {
	if isStderr {
		_, err := s.stderr.WriteString(text)
		return err
	}
	_, err := s.stdout.WriteString(text)
	return err
}

func (s *bashOutputSpool) Finalize() (string, string, error) {
	if s == nil {
		return "", "", nil
	}
	stdoutCloseErr := s.stdout.Close()
	stderrCloseErr := s.stderr.Close()
	closeErr := errors.Join(stdoutCloseErr, stderrCloseErr)

	if s.stdout.Truncated() || s.stderr.Truncated() {
		combined := combineOutput(s.stdout.String(), s.stderr.String())
		return combined, "", closeErr
	}

	stdoutPath := s.stdout.Path()
	stderrPath := s.stderr.Path()
	defer func() {
		if stderrPath == "" {
			return
		}
		_ = os.Remove(stderrPath)
	}()

	if stdoutPath == "" && stderrPath == "" {
		combined := combineOutput(s.stdout.String(), s.stderr.String())
		if len(combined) <= s.threshold {
			return combined, "", closeErr
		}
		if err := ensureBashOutputDir(filepath.Dir(s.outputPath)); err != nil {
			return combined, "", errors.Join(closeErr, err)
		}
		if err := os.WriteFile(s.outputPath, []byte(combined), 0o600); err != nil {
			return combined, "", errors.Join(closeErr, err)
		}
		return formatBashOutputReference(s.outputPath), s.outputPath, closeErr
	}

	if stdoutPath == "" {
		if err := ensureBashOutputDir(filepath.Dir(s.outputPath)); err != nil {
			combined := combineOutput(s.stdout.String(), s.stderr.String())
			return combined, "", errors.Join(closeErr, err)
		}
		out, err := os.OpenFile(s.outputPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			combined := combineOutput(s.stdout.String(), s.stderr.String())
			return combined, "", errors.Join(closeErr, err)
		}
		if err := writeCombinedOutput(out, s.stdout.String(), stderrPath, s.stderr.String()); err != nil {
			_ = bashFileClose(out)
			return "", "", errors.Join(closeErr, err)
		}
		if err := bashFileClose(out); err != nil {
			return "", "", errors.Join(closeErr, err)
		}
		return formatBashOutputReference(s.outputPath), s.outputPath, closeErr
	}

	out, err := os.OpenFile(s.outputPath, os.O_RDWR, 0)
	if err != nil {
		return "", "", errors.Join(closeErr, err)
	}
	stdoutLen, err := trimRightNewlinesInFile(out)
	if err != nil {
		_ = bashFileClose(out)
		return "", "", errors.Join(closeErr, err)
	}
	if err := appendStderr(out, stdoutLen, stderrPath, s.stderr.String()); err != nil {
		_ = bashFileClose(out)
		return "", "", errors.Join(closeErr, err)
	}
	if err := bashFileClose(out); err != nil {
		return "", "", errors.Join(closeErr, err)
	}
	return formatBashOutputReference(s.outputPath), s.outputPath, closeErr
}

func writeCombinedOutput(out *os.File, stdoutText, stderrPath, stderrText string) error {
	stdoutTrim := strings.TrimRight(stdoutText, "\r\n")
	if stdoutTrim != "" {
		if _, err := out.WriteString(stdoutTrim); err != nil {
			return err
		}
	}
	return appendStderr(out, int64(len(stdoutTrim)), stderrPath, stderrText)
}

func appendStderr(out *os.File, stdoutLen int64, stderrPath, stderrText string) error {
	stderrTrim := strings.TrimRight(stderrText, "\r\n")
	hasStderr := stderrTrim != "" || stderrPath != ""
	if !hasStderr {
		return nil
	}
	stderrLen := int64(len(stderrTrim))
	if stderrPath != "" {
		f, err := os.Open(stderrPath)
		if err != nil {
			return err
		}
		defer func() { _ = bashFileClose(f) }()
		size, err := trimmedFileSize(f)
		if err != nil {
			return err
		}
		stderrLen = size
		if _, err := bashFileSeek(f, 0, io.SeekStart); err != nil {
			return err
		}
		if stdoutLen > 0 && stderrLen > 0 {
			if _, err := out.WriteString("\n"); err != nil {
				return err
			}
		}
		if stderrLen > 0 {
			if _, err := io.CopyN(out, f, stderrLen); err != nil {
				return err
			}
		}
		return nil
	}
	if stdoutLen > 0 && stderrLen > 0 {
		if _, err := out.WriteString("\n"); err != nil {
			return err
		}
	}
	if stderrLen > 0 {
		if _, err := out.WriteString(stderrTrim); err != nil {
			return err
		}
	}
	return nil
}

func bashSessionID(ctx context.Context) string {
	const fallback = "default"
	var session string
	if ctx != nil {
		if st, ok := ctx.Value(model.MiddlewareStateKey).(*middleware.State); ok && st != nil {
			if value, ok := st.Values["session_id"]; ok && value != nil {
				if s, err := coerceString(value); err == nil {
					session = s
				}
			}
			if session == "" {
				if value, ok := st.Values["trace.session_id"]; ok && value != nil {
					if s, err := coerceString(value); err == nil {
						session = s
					}
				}
			}
		}
		if session == "" {
			if value, ok := ctx.Value(middleware.TraceSessionIDContextKey).(string); ok {
				session = value
			} else if value, ok := ctx.Value(middleware.SessionIDContextKey).(string); ok {
				session = value
			}
		}
	}
	session = strings.TrimSpace(session)
	if session == "" {
		return fallback
	}
	return session
}

func sanitizePathComponent(value string) string {
	const fallback = "default"
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		return fallback
	}
	return sanitized
}

func bashOutputFilename() string {
	var randBuf [4]byte
	ts := time.Now().UnixNano()
	if _, err := bashRandRead(randBuf[:]); err == nil {
		return fmt.Sprintf("%d-%s.txt", ts, hex.EncodeToString(randBuf[:]))
	}
	return fmt.Sprintf("%d.txt", ts)
}

func ensureBashOutputDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("output directory is empty")
	}
	return os.MkdirAll(dir, 0o700)
}

func openBashOutputFile(path string) (*os.File, string, error) {
	dir := filepath.Dir(path)
	if err := ensureBashOutputDir(dir); err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

func formatBashOutputReference(path string) string {
	return fmt.Sprintf("[Output saved to: %s]", path)
}

func trimmedFileSize(f *os.File) (int64, error) {
	if f == nil {
		return 0, nil
	}
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if size == 0 {
		return 0, nil
	}

	const chunkSize int64 = 1024
	offset := size
	trimmed := size

	for offset > 0 {
		readSize := chunkSize
		if readSize > offset {
			readSize = offset
		}
		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, offset-readSize); err != nil {
			return 0, err
		}
		i := len(buf) - 1
		for i >= 0 {
			if buf[i] != '\n' && buf[i] != '\r' {
				break
			}
			i--
		}
		trimmed = (offset - readSize) + int64(i+1)
		if i >= 0 {
			break
		}
		offset -= readSize
	}
	return trimmed, nil
}

func trimRightNewlinesInFile(f *os.File) (int64, error) {
	if f == nil {
		return 0, nil
	}
	trimmed, err := trimmedFileSize(f)
	if err != nil {
		return 0, err
	}
	if err := f.Truncate(trimmed); err != nil {
		return 0, err
	}
	if _, err := bashFileSeek(f, trimmed, io.SeekStart); err != nil {
		return 0, err
	}
	return trimmed, nil
}

type bashCommandValidator struct {
	mu sync.RWMutex

	maxCommandBytes int
	maxArgs         int
	allowShellMeta  bool
}

func newBashCommandValidator() *bashCommandValidator {
	return &bashCommandValidator{
		maxCommandBytes: 32768,
		maxArgs:         512,
		allowShellMeta:  false,
	}
}

func (v *bashCommandValidator) SetMaxCommandBytes(n int) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.maxCommandBytes = n
	v.mu.Unlock()
}

func (v *bashCommandValidator) SetMaxArgs(n int) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.maxArgs = n
	v.mu.Unlock()
}

func (v *bashCommandValidator) AllowShellMetachars(allow bool) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.allowShellMeta = allow
	v.mu.Unlock()
}

func (v *bashCommandValidator) Validate(input string) error {
	if v == nil {
		return errors.New("bash: validator is nil")
	}
	cmd := strings.TrimSpace(input)
	if cmd == "" {
		return errors.New("bash: empty command")
	}

	v.mu.RLock()
	maxBytes := v.maxCommandBytes
	maxArgs := v.maxArgs
	allowMeta := v.allowShellMeta
	v.mu.RUnlock()

	if maxBytes > 0 && len(cmd) > maxBytes {
		return fmt.Errorf("bash: command too long (%d bytes)", len(cmd))
	}

	if strings.ContainsAny(cmd, "\n\r") {
		return errors.New("bash: multiline command is not allowed")
	}

	if containsControlNonWhitespace(cmd) {
		return errors.New("bash: control characters detected")
	}

	if !allowMeta && strings.ContainsAny(cmd, "|;&><`$") {
		return errors.New("bash: pipe or shell metacharacters are blocked")
	}

	args, err := splitCommand(cmd)
	if err != nil {
		return fmt.Errorf("bash: parse failed: %w", err)
	}
	if len(args) == 0 {
		return errors.New("bash: empty command")
	}
	if maxArgs > 0 && len(args) > maxArgs {
		return fmt.Errorf("bash: too many arguments (%d)", len(args))
	}

	return nil
}

func containsControlNonWhitespace(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}

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
			inSingle = !inSingle
		case r == '"':
			if inSingle {
				current.WriteRune(r)
				continue
			}
			inDouble = !inDouble
		case unicode.IsSpace(r):
			if inSingle || inDouble {
				current.WriteRune(r)
				continue
			}
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if escape {
		return nil, errors.New("unterminated escape")
	}
	if inSingle || inDouble {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return args, nil
}
