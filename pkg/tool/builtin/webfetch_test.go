package toolbuiltin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWebFetchConvertsHTMLAndCaches(t *testing.T) {
	serverCalls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><h1>Hello</h1><p>Welcome to <strong>Agents</strong>.</p></body></html>"))
	}))
	t.Cleanup(server.Close)

	tool := NewWebFetchTool(&WebFetchOptions{
		HTTPClient:        server.Client(),
		CacheTTL:          time.Minute,
		AllowedHosts:      []string{serverHost(t, server.URL)},
		AllowPrivateHosts: true,
	})

	url := strings.Replace(server.URL+"/page", "https://", "http://", 1)
	params := map[string]interface{}{
		"url":    url,
		"prompt": "summarise",
	}

	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success got %#v", res)
	}
	markdown, _ := res.Data.(map[string]interface{})["content_markdown"].(string)
	trimmed := strings.TrimSpace(markdown)
	if !strings.HasPrefix(trimmed, "#") {
		t.Fatalf("markdown missing heading marker: %q", trimmed)
	}
	if !strings.Contains(trimmed, "Welcome") {
		t.Fatalf("markdown missing body: %q", trimmed)
	}

	res2, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("second execute failed: %v", err)
	}
	if serverCalls != 1 {
		t.Fatalf("expected 1 upstream call got %d", serverCalls)
	}
	data := res2.Data.(map[string]interface{})
	cached, _ := data["from_cache"].(bool)
	if !cached {
		t.Fatalf("expected cache hit metadata")
	}
}

func TestWebFetchRejectsBlockedHosts(t *testing.T) {
	tool := NewWebFetchTool(nil)
	params := map[string]interface{}{
		"url":    "https://127.0.0.1/secret",
		"prompt": "noop",
	}
	if _, err := tool.Execute(context.Background(), params); err == nil {
		t.Fatalf("expected blocked host error")
	}
}

func TestWebFetchReturnsRedirectNotice(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.org/article", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	tool := NewWebFetchTool(&WebFetchOptions{
		HTTPClient:        server.Client(),
		AllowedHosts:      []string{serverHost(t, server.URL)},
		AllowPrivateHosts: true,
	})
	params := map[string]interface{}{
		"url":    server.URL,
		"prompt": "redirect",
	}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("expected redirect result, got error %v", err)
	}
	if res.Success {
		t.Fatalf("redirect response should not be marked successful")
	}
	if !strings.HasPrefix(res.Output, redirectNoticePrefix) {
		t.Fatalf("expected redirect prefix, got %q", res.Output)
	}
}

func TestWebFetchTimeout(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	tool := NewWebFetchTool(&WebFetchOptions{
		HTTPClient:        server.Client(),
		Timeout:           50 * time.Millisecond,
		AllowedHosts:      []string{serverHost(t, server.URL)},
		AllowPrivateHosts: true,
	})

	params := map[string]interface{}{
		"url":    server.URL,
		"prompt": "timeout",
	}
	if _, err := tool.Execute(context.Background(), params); err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestHtmlToMarkdownFallback(t *testing.T) {
	raw := "<p>Hello <em>world</em></p>"
	got := htmlToMarkdown(raw)
	if !strings.Contains(got, "Hello* world*") {
		t.Fatalf("expected formatted sentence, got %q", got)
	}
}

func serverHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	return u.Hostname()
}

func TestWebFetchNormaliseURL(t *testing.T) {
	tool := NewWebFetchTool(nil)
	got, err := tool.normaliseURL("http://example.com/docs")
	if err != nil {
		t.Fatalf("normaliseURL failed: %v", err)
	}
	if !strings.HasPrefix(got, "https://") {
		t.Fatalf("expected https upgrade, got %q", got)
	}
	if _, err := tool.normaliseURL("ftp://example.com"); err == nil {
		t.Fatalf("expected scheme error")
	}
	if _, err := tool.normaliseURL("https://"); err == nil {
		t.Fatalf("expected host error")
	}
}

func TestStringValueCoercion(t *testing.T) {
	str, err := stringValue(json.Number("42"))
	if err != nil || str != "42" {
		t.Fatalf("stringValue failed: %q %v", str, err)
	}
	if _, err := stringValue(123); err == nil {
		t.Fatalf("expected error for non-string")
	}
}

func TestFetchCacheStoresAndExpires(t *testing.T) {
	cache := newFetchCache(10 * time.Millisecond)
	entry := &fetchResult{URL: "https://example.com", Status: 200, Body: []byte("ok")}
	cache.Set("key", entry)
	got, ok := cache.Get("key")
	if !ok || string(got.Body) != "ok" {
		t.Fatalf("cache get failed: %v %v", ok, got)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := cache.Get("key"); ok {
		t.Fatalf("expected cache entry to expire")
	}
}

func TestHtmlToMarkdownAdvanced(t *testing.T) {
	input := `<section><h2>Guide</h2><ul><li>Intro</li><li><strong>Deep</strong></li></ul><ol><li>First</li></ol><pre><code>fmt.Println("ok")</code></pre><p>Visit <a href="https://example.com">docs</a><br/>line two</p><img src="https://img.dev/a.png" alt="Shot"/><script>alert('x')</script></section>`
	output := htmlToMarkdown(input)
	condensed := strings.ReplaceAll(output, " ", "")
	if !strings.Contains(condensed, "##Guide") {
		t.Fatalf("missing heading: %q", output)
	}
	if !strings.Contains(output, "-") || !strings.Contains(output, "1.") {
		t.Fatalf("list markers missing: %q", output)
	}
	if !strings.Contains(output, "```") {
		t.Fatalf("pre block missing: %q", output)
	}
	if !strings.Contains(condensed, "[docs](https://example.com)") {
		t.Fatalf("link missing: %q", output)
	}
	if strings.Contains(output, "alert") {
		t.Fatalf("script content should be stripped: %q", output)
	}
}

func TestHtmlToMarkdownInlineElements(t *testing.T) {
	input := `<div><strong>Bold</strong> <em>lite</em><code>inline</code><br/><img src="https://img.dev/p.png" alt="Cover"/></div>`
	output := htmlToMarkdown(input)
	condensed := strings.ReplaceAll(output, " ", "")
	if !strings.Contains(condensed, "**Bold**") {
		t.Fatalf("missing strong formatting: %q", output)
	}
	if !strings.Contains(condensed, "*lite*") {
		t.Fatalf("missing emphasis: %q", output)
	}
	if !strings.Contains(condensed, "`inline`") {
		t.Fatalf("missing inline code: %q", output)
	}
	if !strings.Contains(condensed, "![Cover](https://img.dev/p.png)") {
		t.Fatalf("missing image markdown: %q", output)
	}
}

func TestWebFetchRejectsLargeResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("A", 32)))
	}))
	t.Cleanup(server.Close)

	tool := NewWebFetchTool(&WebFetchOptions{
		HTTPClient:        server.Client(),
		MaxContentSize:    8,
		AllowedHosts:      []string{serverHost(t, server.URL)},
		AllowPrivateHosts: true,
	})
	params := map[string]interface{}{"url": server.URL, "prompt": "limit"}
	if _, err := tool.Execute(context.Background(), params); err == nil {
		t.Fatalf("expected size limit error")
	}
}

func TestWebFetchMetadataAndHelpers(t *testing.T) {
	tool := NewWebFetchTool(nil)
	if tool.Name() != "WebFetch" {
		t.Fatalf("unexpected name")
	}
	if tool.Description() == "" || tool.Schema() == nil {
		t.Fatalf("schema/description missing")
	}
	longText := strings.Repeat("line\n", markdownSnippetMaxLines+5)
	summary := summariseMarkdown(longText)
	if !strings.HasSuffix(summary, "...") {
		t.Fatalf("expected truncation, got %q", summary)
	}
	if nameToHeadingLevel("h4") != 4 || nameToHeadingLevel("x") != 6 {
		t.Fatalf("unexpected heading level")
	}
}

func TestExtractNonEmptyStringValidation(t *testing.T) {
	if _, err := extractNonEmptyString(map[string]interface{}{}, "key"); err == nil {
		t.Fatalf("expected missing key error")
	}
	if _, err := extractNonEmptyString(map[string]interface{}{"key": ""}, "key"); err == nil {
		t.Fatalf("expected empty value error")
	}
}

func TestRedirectPolicyLimits(t *testing.T) {
	tool := NewWebFetchTool(nil)
	policy := tool.redirectPolicy()
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	via := make([]*http.Request, maxFetchRedirects)
	for i := range via {
		via[i] = httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	}
	if err := policy(req, via); err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("expected redirect limit error, got %v", err)
	}
}

func TestHostRedirectErrorString(t *testing.T) {
	err := (&hostRedirectError{target: "https://next"}).Error()
	if !strings.Contains(err, "https://next") {
		t.Fatalf("unexpected error string %q", err)
	}
}

func TestReadBoundedError(t *testing.T) {
	reader := failingReader{}
	if _, err := readBounded(reader, 8); err == nil {
		t.Fatalf("expected read error")
	}
}

func TestStringValueBytes(t *testing.T) {
	str, err := stringValue([]byte("data"))
	if err != nil || str != "data" {
		t.Fatalf("unexpected result %q %v", str, err)
	}
}

func TestHostValidatorWhitelist(t *testing.T) {
	validator := newHostValidator([]string{"example.com"}, nil, false)
	if err := validator.Validate("bad.com"); err == nil {
		t.Fatalf("expected whitelist failure")
	}
}

func TestExtractURLValidation(t *testing.T) {
	tool := NewWebFetchTool(nil)
	if _, err := tool.extractURL(map[string]interface{}{}); err == nil {
		t.Fatalf("expected missing url error")
	}
	if _, err := tool.extractURL(map[string]interface{}{"url": " "}); err == nil {
		t.Fatalf("expected empty url error")
	}
}

func TestFetchCacheMissAndNilSet(t *testing.T) {
	cache := newFetchCache(time.Minute)
	if _, ok := cache.Get("missing"); ok {
		t.Fatalf("expected cache miss")
	}
	cache.Set("ignored", nil)
}

func TestHtmlToMarkdownParseError(t *testing.T) {
	out := htmlToMarkdown("<<<")
	if out != "<<<" {
		t.Fatalf("expected raw fallback, got %q", out)
	}
}

func TestHostValidatorAllowsPrivate(t *testing.T) {
	validator := newHostValidator(nil, nil, true)
	if err := validator.Validate("127.0.0.1"); err != nil {
		t.Fatalf("expected private host allowance: %v", err)
	}
}

func TestWebFetchExecuteValidations(t *testing.T) {
	tool := NewWebFetchTool(nil)
	if _, err := tool.Execute(nil, nil); err == nil {
		t.Fatalf("expected context error")
	}
	if _, err := tool.Execute(context.Background(), nil); err == nil {
		t.Fatalf("expected params error")
	}
	if _, err := tool.Execute(context.Background(), map[string]interface{}{"url": "https://example.com"}); err == nil {
		t.Fatalf("expected prompt validation error")
	}
}

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("boom")
}
