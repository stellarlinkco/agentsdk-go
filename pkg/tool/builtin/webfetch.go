package toolbuiltin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const (
	webFetchDescription = "\n- Fetches content from a specified URL and processes it using an AI model\n- Takes a URL and a prompt as input\n- Fetches the URL content, converts HTML to markdown\n- Processes the content with the prompt using a small, fast model\n- Returns the model's response about the content\n- Use this tool when you need to retrieve and analyze web content\n\nUsage notes:\n  - IMPORTANT: If an MCP-provided web fetch tool is available, prefer using that tool instead of this one, as it may have fewer restrictions. All MCP-provided tools start with \"mcp__\".\n  - The URL must be a fully-formed valid URL\n  - HTTP URLs will be automatically upgraded to HTTPS\n  - The prompt should describe what information you want to extract from the page\n  - This tool is read-only and does not modify any files\n  - Results may be summarized if the content is very large\n  - Includes a self-cleaning 15-minute cache for faster responses when repeatedly accessing the same URL\n  - When a URL redirects to a different host, the tool will inform you and provide the redirect URL in a special format. You should then make a new WebFetch request with the redirect URL to fetch the content.\n"

	defaultFetchTimeout     = 15 * time.Second
	maxFetchTimeout         = 60 * time.Second
	defaultFetchCacheTTL    = 15 * time.Minute
	defaultFetchMaxBytes    = 2 << 20 // 2 MiB
	maxFetchRedirects       = 5
	defaultFetchUserAgent   = "agentsdk-webfetch/1.0"
	redirectNoticePrefix    = "redirect://"
	markdownSnippetMaxLines = 12
)

var webFetchSchema = &tool.JSONSchema{
	Type: "object",
	Properties: map[string]interface{}{
		"url": map[string]interface{}{
			"type":        "string",
			"format":      "uri",
			"description": "The URL to fetch content from",
		},
		"prompt": map[string]interface{}{
			"type":        "string",
			"description": "The prompt to run on the fetched content",
		},
	},
	Required: []string{"url", "prompt"},
}

// WebFetchOptions configures WebFetchTool behaviour.
type WebFetchOptions struct {
	HTTPClient        *http.Client
	Timeout           time.Duration
	CacheTTL          time.Duration
	MaxContentSize    int64
	AllowedHosts      []string
	BlockedHosts      []string
	AllowPrivateHosts bool
}

// WebFetchTool fetches remote web pages and returns Markdown content.
type WebFetchTool struct {
	client    *http.Client
	timeout   time.Duration
	maxBytes  int64
	cache     *fetchCache
	validator hostValidator
	now       func() time.Time
}

// NewWebFetchTool builds a WebFetchTool with sane defaults.
func NewWebFetchTool(opts *WebFetchOptions) *WebFetchTool {
	cfg := WebFetchOptions{}
	if opts != nil {
		cfg = *opts
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	if timeout > maxFetchTimeout {
		timeout = maxFetchTimeout
	}
	maxBytes := cfg.MaxContentSize
	if maxBytes <= 0 {
		maxBytes = defaultFetchMaxBytes
	}
	cacheTTL := cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultFetchCacheTTL
	}

	client := cloneHTTPClient(cfg.HTTPClient)
	client.Timeout = timeout

	tool := &WebFetchTool{
		client:    client,
		timeout:   timeout,
		maxBytes:  maxBytes,
		cache:     newFetchCache(cacheTTL),
		validator: newHostValidator(cfg.AllowedHosts, cfg.BlockedHosts, cfg.AllowPrivateHosts),
		now:       time.Now,
	}
	tool.client.CheckRedirect = tool.redirectPolicy()
	return tool
}

func (w *WebFetchTool) Name() string { return "WebFetch" }

func (w *WebFetchTool) Description() string { return webFetchDescription }

func (w *WebFetchTool) Schema() *tool.JSONSchema { return webFetchSchema }

// Execute fetches the requested URL, converts it to Markdown and returns metadata.
func (w *WebFetchTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if w == nil || w.client == nil {
		return nil, errors.New("web fetch tool is not initialised")
	}
	if params == nil {
		return nil, errors.New("params is nil")
	}

	rawURL, err := w.extractURL(params)
	if err != nil {
		return nil, err
	}
	prompt, err := extractNonEmptyString(params, "prompt")
	if err != nil {
		return nil, err
	}

	normalized, err := w.normaliseURL(rawURL)
	if err != nil {
		return nil, err
	}

	reqCtx := ctx
	var cancel context.CancelFunc
	if w.timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, w.timeout)
		defer cancel()
	}

	fetched, notice, err := w.fetch(reqCtx, normalized)
	if err != nil {
		return nil, err
	}
	if notice != nil {
		return &tool.ToolResult{
			Success: false,
			Output:  redirectNoticePrefix + notice.URL,
			Data: map[string]interface{}{
				"redirect_url": notice.URL,
				"reason":       "cross-host redirect",
			},
		}, nil
	}

	markdown := htmlToMarkdown(string(fetched.Body))
	snippet := summariseMarkdown(markdown)

	result := &tool.ToolResult{
		Success: true,
		Output:  fmt.Sprintf("Fetched %s (%d bytes)\n%s", fetched.URL, len(fetched.Body), snippet),
		Data: map[string]interface{}{
			"url":              fetched.URL,
			"requested_url":    normalized,
			"status":           fetched.Status,
			"content_markdown": markdown,
			"prompt":           prompt,
			"from_cache":       fetched.FromCache,
			"fetched_at":       w.now().UTC().Format(time.RFC3339),
			"content_bytes":    len(fetched.Body),
		},
	}
	return result, nil
}

func (w *WebFetchTool) extractURL(params map[string]interface{}) (string, error) {
	raw, ok := params["url"]
	if !ok {
		return "", errors.New("url is required")
	}
	value, err := stringValue(raw)
	if err != nil {
		return "", fmt.Errorf("url must be string: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("url cannot be empty")
	}
	return value, nil
}

func (w *WebFetchTool) normaliseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		return "", errors.New("url scheme is required")
	}
	switch scheme {
	case "http":
		parsed.Scheme = "https"
	case "https":
	default:
		return "", fmt.Errorf("unsupported url scheme %s", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("url host is required")
	}
	parsed.User = nil
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	host := parsed.Hostname()
	if err := w.validator.Validate(host); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func (w *WebFetchTool) fetch(ctx context.Context, normalized string) (*fetchResult, *redirectNotice, error) {
	if cached, ok := w.cache.Get(normalized); ok {
		clone := *cached
		clone.Body = append([]byte(nil), cached.Body...)
		clone.FromCache = true
		return &clone, nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", defaultFetchUserAgent)
	resp, err := w.client.Do(req)
	if err != nil {
		if notice := detectRedirectNotice(err); notice != nil {
			return nil, notice, nil
		}
		return nil, nil, fmt.Errorf("fetch url: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("fetch failed with status %d", resp.StatusCode)
	}

	data, err := readBounded(resp.Body, w.maxBytes)
	if err != nil {
		return nil, nil, err
	}

	result := &fetchResult{
		URL:    resp.Request.URL.String(),
		Status: resp.StatusCode,
		Body:   data,
	}
	w.cache.Set(normalized, result)
	return result, nil, nil
}

type fetchResult struct {
	URL       string
	Status    int
	Body      []byte
	FromCache bool
}

type redirectNotice struct {
	URL string
}

func (w *WebFetchTool) redirectPolicy() func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxFetchRedirects {
			return fmt.Errorf("too many redirects")
		}
		if len(via) == 0 {
			return nil
		}
		original := hostWithoutPort(via[0].URL.Host)
		next := hostWithoutPort(req.URL.Host)
		if !strings.EqualFold(original, next) {
			return &hostRedirectError{target: req.URL.String()}
		}
		return nil
	}
}

type hostRedirectError struct {
	target string
}

func (e *hostRedirectError) Error() string {
	return fmt.Sprintf("redirected to disallowed host: %s", e.target)
}

func detectRedirectNotice(err error) *redirectNotice {
	var redirectErr *hostRedirectError
	if errors.As(err, &redirectErr) {
		return &redirectNotice{URL: redirectErr.target}
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if errors.As(urlErr.Err, &redirectErr) {
			return &redirectNotice{URL: redirectErr.target}
		}
	}
	return nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = defaultFetchMaxBytes
	}
	reader := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes limit", limit)
	}
	return data, nil
}

func cloneHTTPClient(c *http.Client) *http.Client {
	if c == nil {
		return &http.Client{}
	}
	clone := *c
	if clone.Transport == nil {
		clone.Transport = http.DefaultTransport
	}
	return &clone
}

type fetchCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	expires time.Time
	result  fetchResult
}

func newFetchCache(ttl time.Duration) *fetchCache {
	return &fetchCache{
		ttl:     ttl,
		entries: make(map[string]cacheEntry),
	}
}

func (c *fetchCache) Get(key string) (*fetchResult, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeLocked()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(c.entries, key)
		}
		return nil, false
	}
	res := entry.result
	return &res, true
}

func (c *fetchCache) Set(key string, result *fetchResult) {
	if c == nil || result == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeLocked()
	clone := fetchResult{
		URL:    result.URL,
		Status: result.Status,
		Body:   append([]byte(nil), result.Body...),
	}
	c.entries[key] = cacheEntry{
		expires: time.Now().Add(c.ttl),
		result:  clone,
	}
}

func (c *fetchCache) purgeLocked() {
	if c == nil || len(c.entries) == 0 {
		return
	}
	now := time.Now()
	for k, v := range c.entries {
		if now.After(v.expires) {
			delete(c.entries, k)
		}
	}
}

func hostWithoutPort(hostport string) string {
	host := hostport
	if strings.Contains(hostport, ":") {
		if parsedHost, _, err := net.SplitHostPort(hostport); err == nil {
			host = parsedHost
		}
	}
	return host
}

type hostValidator struct {
	allowed      map[string]struct{}
	blocked      map[string]struct{}
	allowPrivate bool
}

var defaultBlockedHosts = map[string]struct{}{
	"localhost":                {},
	"127.0.0.1":                {},
	"::1":                      {},
	"metadata.google.internal": {},
	"169.254.169.254":          {},
}

func newHostValidator(allowed, blocked []string, allowPrivate bool) hostValidator {
	hv := hostValidator{
		allowed:      sliceToSet(allowed),
		blocked:      sliceToSet(blocked),
		allowPrivate: allowPrivate,
	}
	return hv
}

func sliceToSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		trimmed := strings.ToLower(strings.TrimSpace(v))
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return set
}

func (h hostValidator) Validate(host string) error {
	hostname := strings.ToLower(hostWithoutPort(host))
	if hostname == "" {
		return errors.New("host cannot be empty")
	}
	if len(h.allowed) > 0 {
		matched := false
		for allow := range h.allowed {
			if domainMatches(hostname, allow) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("host %s is not in whitelist", hostname)
		}
	}
	if !h.allowPrivate {
		if _, ok := defaultBlockedHosts[hostname]; ok {
			return fmt.Errorf("host %s is blocked", hostname)
		}
		if ip := net.ParseIP(hostname); ip != nil {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				return fmt.Errorf("ip %s is not reachable", hostname)
			}
		}
	}
	for block := range h.blocked {
		if domainMatches(hostname, block) {
			return fmt.Errorf("host %s is blocked", hostname)
		}
	}
	return nil
}

func domainMatches(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if host == domain {
		return true
	}
	if strings.HasSuffix(host, "."+domain) {
		return true
	}
	return false
}

func extractNonEmptyString(params map[string]interface{}, key string) (string, error) {
	raw, ok := params[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	value, err := stringValue(raw)
	if err != nil {
		return "", fmt.Errorf("%s must be string: %w", key, err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s cannot be empty", key)
	}
	return value, nil
}

// htmlToMarkdown converts a limited subset of HTML nodes into Markdown.
func htmlToMarkdown(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	node, err := xhtml.Parse(strings.NewReader(trimmed))
	if err != nil {
		return strings.TrimSpace(html.UnescapeString(trimmed))
	}
	builder := &markdownBuilder{}
	builder.walk(node)
	result := strings.TrimSpace(builder.String())
	if result == "" {
		return strings.TrimSpace(html.UnescapeString(trimmed))
	}
	return result
}

type markdownBuilder struct {
	strings.Builder
	listStack []listContext
	linkStack []string
	inPre     bool
}

type listContext struct {
	ordered bool
	index   int
}

func (m *markdownBuilder) walk(n *xhtml.Node) {
	switch n.Type {
	case xhtml.TextNode:
		m.writeText(n.Data)
	case xhtml.ElementNode:
		if shouldSkipNode(n.Data) {
			return
		}
		m.handleStart(n)
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			m.walk(child)
		}
		m.handleEnd(n)
	default:
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			m.walk(child)
		}
	}
}

func shouldSkipNode(name string) bool {
	lower := strings.ToLower(name)
	return lower == "script" || lower == "style" || lower == "noscript"
}

func (m *markdownBuilder) handleStart(n *xhtml.Node) {
	name := strings.ToLower(n.Data)
	switch name {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		m.ensureBlankLine()
		level := nameToHeadingLevel(name)
		m.WriteString(strings.Repeat("#", level))
		m.WriteString(" ")
	case "p", "div", "section", "article":
		m.ensureBlankLine()
	case "br":
		m.WriteString("\n")
	case "pre":
		if !m.inPre {
			m.ensureBlankLine()
			m.WriteString("```\n")
			m.inPre = true
		}
	case "code":
		if !m.inPre {
			m.WriteString("`")
		}
	case "strong", "b":
		m.WriteString("**")
	case "em", "i":
		m.WriteString("*")
	case "ul":
		m.listStack = append(m.listStack, listContext{})
		m.ensureBlankLine()
	case "ol":
		m.listStack = append(m.listStack, listContext{ordered: true})
		m.ensureBlankLine()
	case "li":
		m.startListItem()
	case "a":
		if href := attrValue(n, "href"); href != "" {
			m.linkStack = append(m.linkStack, href)
			m.WriteString("[")
		}
	case "img":
		alt := attrValue(n, "alt")
		src := attrValue(n, "src")
		if src != "" {
			if alt == "" {
				alt = src
			}
			m.WriteString("![")
			m.WriteString(alt)
			m.WriteString("](")
			m.WriteString(src)
			m.WriteString(")")
		}
	}
}

func (m *markdownBuilder) handleEnd(n *xhtml.Node) {
	name := strings.ToLower(n.Data)
	switch name {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p", "div", "section", "article":
		m.WriteString("\n")
	case "pre":
		if m.inPre {
			if !strings.HasSuffix(m.String(), "\n") {
				m.WriteString("\n")
			}
			m.WriteString("```\n")
			m.inPre = false
		}
	case "code":
		if !m.inPre {
			m.WriteString("`")
		}
	case "strong", "b":
		m.WriteString("**")
	case "em", "i":
		m.WriteString("*")
	case "ul", "ol":
		if len(m.listStack) > 0 {
			m.listStack = m.listStack[:len(m.listStack)-1]
		}
		m.WriteString("\n")
	case "a":
		if len(m.linkStack) > 0 {
			href := m.linkStack[len(m.linkStack)-1]
			m.linkStack = m.linkStack[:len(m.linkStack)-1]
			m.WriteString("](")
			m.WriteString(href)
			m.WriteString(")")
		}
	}
}

func (m *markdownBuilder) writeText(text string) {
	if strings.TrimSpace(text) == "" && !m.inPre {
		return
	}
	if m.inPre {
		m.WriteString(text)
		return
	}
	cleaned := collapseSpaces(html.UnescapeString(text))
	if cleaned == "" {
		return
	}
	if m.Len() > 0 && !strings.HasSuffix(m.String(), "\n") {
		m.WriteString(" ")
	}
	m.WriteString(cleaned)
}

func (m *markdownBuilder) ensureBlankLine() {
	if m.Len() == 0 {
		return
	}
	if !strings.HasSuffix(m.String(), "\n\n") {
		if strings.HasSuffix(m.String(), "\n") {
			m.WriteString("\n")
		} else {
			m.WriteString("\n\n")
		}
	}
}

func (m *markdownBuilder) startListItem() {
	if len(m.listStack) == 0 {
		m.listStack = append(m.listStack, listContext{})
	}
	ctx := &m.listStack[len(m.listStack)-1]
	indent := strings.Repeat("  ", len(m.listStack)-1)
	marker := "- "
	if ctx.ordered {
		ctx.index++
		marker = fmt.Sprintf("%d. ", ctx.index)
	}
	if !strings.HasSuffix(m.String(), "\n") {
		m.WriteString("\n")
	}
	m.WriteString(indent)
	m.WriteString(marker)
}

func nameToHeadingLevel(name string) int {
	switch name {
	case "h1":
		return 1
	case "h2":
		return 2
	case "h3":
		return 3
	case "h4":
		return 4
	case "h5":
		return 5
	default:
		return 6
	}
}

func attrValue(n *xhtml.Node, key string) string {
	lower := strings.ToLower(key)
	for _, attr := range n.Attr {
		if strings.ToLower(attr.Key) == lower {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func collapseSpaces(input string) string {
	fields := strings.Fields(input)
	return strings.Join(fields, " ")
}

func summariseMarkdown(md string) string {
	if md == "" {
		return ""
	}
	lines := strings.Split(md, "\n")
	if len(lines) > markdownSnippetMaxLines {
		lines = lines[:markdownSnippetMaxLines]
		lines = append(lines, "...")
	}
	trimmed := strings.TrimSpace(strings.Join(lines, "\n"))
	return trimmed
}

func stringValue(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case fmt.Stringer:
		return v.String(), nil
	case []byte:
		return string(v), nil
	case json.Number:
		return v.String(), nil
	default:
		return "", fmt.Errorf("expected string got %T", value)
	}
}
