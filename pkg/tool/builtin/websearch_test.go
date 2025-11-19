package toolbuiltin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

func TestWebSearchFiltersDomains(t *testing.T) {
	html := ddgHTML(
		`<div class="result"><a class="result__a" href="https://news.example.com/doc">Doc</a><div class="result__snippet">first</div></div>`,
		`<div class="result"><a class="result__a" href="https://example.org/post">Other</a><div class="result__snippet">second</div></div>`,
	)
	tool := newTestWebSearchTool(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("q") != "latest" {
			t.Fatalf("unexpected query %s", r.PostForm.Get("q"))
		}
		_, _ = w.Write([]byte(html))
	}, &WebSearchOptions{MaxResults: 5})

	params := map[string]interface{}{
		"query":           "latest",
		"allowed_domains": []interface{}{"example.org"},
	}

	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	data := res.Data.(map[string]interface{})
	results := data["results"].([]SearchResult)
	if len(results) != 1 || results[0].URL != "https://example.org/post" {
		t.Fatalf("unexpected filter result %#v", results)
	}
}

func TestWebSearchBlockedDomain(t *testing.T) {
	html := ddgHTML(
		`<div class="result"><a class="result__a" href="https://bad.example.com/a">A</a></div>`,
		`<div class="result"><a class="result__a" href="https://good.dev/b">B</a><div class="result__snippet">desc</div></div>`,
	)
	tool := newTestWebSearchTool(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}, nil)

	params := map[string]interface{}{
		"query":           "security",
		"blocked_domains": []string{"example.com"},
	}

	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	results := res.Data.(map[string]interface{})["results"].([]SearchResult)
	if len(results) != 1 || results[0].URL != "https://good.dev/b" {
		t.Fatalf("blocked filter failed %#v", results)
	}
}

func TestWebSearchFallbackURL(t *testing.T) {
	html := ddgHTML(
		`<div class="result"><a class="result__a">Title</a><span class="result__url">https://fallback.dev/path</span><div class="result__snippet"> info </div></div>`,
	)
	tool := newTestWebSearchTool(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}, nil)

	res, err := tool.Execute(context.Background(), map[string]interface{}{"query": "fallback"})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	results := res.Data.(map[string]interface{})["results"].([]SearchResult)
	if len(results) != 1 || results[0].URL != "https://fallback.dev/path" {
		t.Fatalf("fallback parse failed %#v", results)
	}
	if results[0].Snippet != "info" {
		t.Fatalf("unexpected snippet %q", results[0].Snippet)
	}
}

func TestWebSearchShortQuery(t *testing.T) {
	tool := NewWebSearchTool(nil)
	params := map[string]interface{}{"query": "a"}
	if _, err := tool.Execute(context.Background(), params); err == nil {
		t.Fatalf("expected short query error")
	}
}

func TestWebSearchTimeout(t *testing.T) {
	tool := newTestWebSearchTool(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(ddgHTML(`<div class="result"><a class="result__a" href="https://slow.dev">Slow</a></div>`)))
	}, &WebSearchOptions{Timeout: 50 * time.Millisecond})

	params := map[string]interface{}{"query": "timeout"}
	if _, err := tool.Execute(context.Background(), params); err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestWebSearchFormatsEmptyResults(t *testing.T) {
	tool := newTestWebSearchTool(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>No hits</body></html>"))
	}, nil)

	params := map[string]interface{}{"query": "nothing"}
	res, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(res.Output, "No results") {
		t.Fatalf("unexpected output: %s", res.Output)
	}
}

func TestWebSearchDomainListValidation(t *testing.T) {
	tool := NewWebSearchTool(nil)
	params := map[string]interface{}{
		"query":           "news",
		"allowed_domains": "example.com",
	}
	if _, err := tool.Execute(context.Background(), params); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestWebSearchHTTPError(t *testing.T) {
	tool := newTestWebSearchTool(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}, nil)

	params := map[string]interface{}{"query": "errors"}
	if _, err := tool.Execute(context.Background(), params); err == nil {
		t.Fatalf("expected upstream error")
	}
}

func TestWebSearchSendsPOSTForm(t *testing.T) {
	tool := newTestWebSearchTool(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), duckDuckGoFormContentType) {
			t.Fatalf("unexpected content type %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("User-Agent") != defaultSearchUserAgent {
			t.Fatalf("unexpected user agent %s", r.Header.Get("User-Agent"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("q") != "network" {
			t.Fatalf("unexpected query value %s", r.PostForm.Get("q"))
		}
		if r.PostForm.Get("kl") != "us-en" {
			t.Fatalf("missing kl param")
		}
		_, _ = w.Write([]byte(ddgHTML(`<div class="result"><a class="result__a" href="https://net.dev">Net</a></div>`)))
	}, nil)

	if _, err := tool.Execute(context.Background(), map[string]interface{}{"query": "network"}); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestFormatSearchOutput(t *testing.T) {
	results := []SearchResult{
		{Title: "One", URL: "https://one.dev", Snippet: "alpha"},
		{Title: "Two", URL: "https://two.dev"},
	}
	text := formatSearchOutput("query", results)
	if !strings.Contains(text, "1. One") || !strings.Contains(text, "2. Two") {
		t.Fatalf("unexpected formatted output: %s", text)
	}
}

func TestWebSearchMetadata(t *testing.T) {
	tool := NewWebSearchTool(nil)
	if tool.Name() != "WebSearch" {
		t.Fatalf("unexpected name")
	}
	if tool.Description() == "" || tool.Schema() == nil {
		t.Fatalf("missing metadata")
	}
}

func TestNormaliseDomainsHelper(t *testing.T) {
	got := normaliseDomains([]string{"Example.com", "", "example.com"})
	if len(got) != 1 || got[0] != "example.com" {
		t.Fatalf("unexpected domains %v", got)
	}
	if normaliseDomains(nil) != nil {
		t.Fatalf("nil input should return nil")
	}
}

func TestExtractHostHelper(t *testing.T) {
	if host := extractHost("https://example.com/path"); host != "example.com" {
		t.Fatalf("unexpected host %s", host)
	}
	if host := extractHost("://bad"); host != "" {
		t.Fatalf("expected empty host")
	}
}

func TestWebSearchExecuteValidation(t *testing.T) {
	tool := NewWebSearchTool(nil)
	if _, err := tool.Execute(nil, map[string]interface{}{"query": "x"}); err == nil {
		t.Fatalf("expected context error")
	}
	if _, err := tool.Execute(context.Background(), nil); err == nil {
		t.Fatalf("expected params error")
	}
}

func TestNodeHasClass(t *testing.T) {
	if nodeHasClass(nil, "result") {
		t.Fatalf("nil node should not have class")
	}

	nodeWithoutAttr := &xhtml.Node{Type: xhtml.ElementNode, Data: "div"}
	if nodeHasClass(nodeWithoutAttr, "foo") {
		t.Fatalf("node without class attribute should return false")
	}

	nodeWithMultiple := &xhtml.Node{
		Type: xhtml.ElementNode,
		Data: "div",
		Attr: []xhtml.Attribute{{Key: "class", Val: "foo bar\tbaz"}},
	}
	if !nodeHasClass(nodeWithMultiple, "bar") {
		t.Fatalf("expected to match one of multiple classes")
	}
	if nodeHasClass(nodeWithMultiple, "ba") {
		t.Fatalf("should not match partial class names")
	}

	nodeWithEmpty := &xhtml.Node{
		Type: xhtml.ElementNode,
		Data: "div",
		Attr: []xhtml.Attribute{{Key: "class", Val: ""}},
	}
	if nodeHasClass(nodeWithEmpty, "") {
		t.Fatalf("empty class attribute should remain false")
	}
}

func TestDeduplicateResults(t *testing.T) {
	if res := deduplicateResults(nil); res != nil {
		t.Fatalf("nil slice should return nil")
	}

	onlyEmpty := []SearchResult{
		{Title: "A"},
		{Title: "B", URL: ""},
	}
	if res := deduplicateResults(onlyEmpty); res != nil {
		t.Fatalf("results without URLs should yield nil, got %#v", res)
	}

	input := []SearchResult{
		{Title: "First", URL: "https://example.com/a"},
		{Title: "Dup", URL: "https://example.com/a"},
		{Title: "MissingURL", URL: ""},
		{Title: "Second", URL: "https://example.com/b"},
	}
	got := deduplicateResults(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique results, got %#v", got)
	}
	if got[0].Title != "First" || got[1].Title != "Second" {
		t.Fatalf("unexpected order after dedupe %#v", got)
	}
}

func TestCollectNodeText(t *testing.T) {
	var builder strings.Builder
	collectNodeText(nil, &builder)
	if builder.Len() != 0 {
		t.Fatalf("nil node should not write text")
	}

	textNode := &xhtml.Node{Type: xhtml.TextNode, Data: "  spaced text "}
	builder.Reset()
	collectNodeText(textNode, &builder)
	if builder.String() != textNode.Data {
		t.Fatalf("expected raw text node data, got %q", builder.String())
	}

	root := &xhtml.Node{Type: xhtml.ElementNode, Data: "div"}
	first := &xhtml.Node{Type: xhtml.TextNode, Data: "Hello "}
	br := &xhtml.Node{Type: xhtml.ElementNode, Data: "br"}
	span := &xhtml.Node{Type: xhtml.ElementNode, Data: "span"}
	spanText := &xhtml.Node{Type: xhtml.TextNode, Data: " world "}
	root.FirstChild = first
	first.NextSibling = br
	br.NextSibling = span
	span.FirstChild = spanText

	builder.Reset()
	collectNodeText(root, &builder)
	if got := collapseWhitespace(builder.String()); got != "Hello world" {
		t.Fatalf("unexpected collected text %q", got)
	}
}

func TestCleanResultURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty input", raw: "   ", want: ""},
		{name: "non http scheme", raw: "ftp://example.com/file", want: ""},
		{name: "invalid url", raw: "%", want: ""},
		{name: "missing host", raw: "https:///nohost", want: ""},
		{name: "valid https", raw: " https://example.com/path?q=1#frag ", want: "https://example.com/path?q=1"},
		{name: "decoded encoded url", raw: "https%3A%2F%2Fexample.com%2Fdoc%3Fq%3Da%2Bb#section", want: "https://example.com/doc?q=a+b"},
		{name: "special characters", raw: "https://example.com/a%20b?foo=bar%2Bbaz#frag", want: "https://example.com/a%20b?foo=bar+baz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanResultURL(tt.raw); got != tt.want {
				t.Fatalf("cleanResultURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func ddgHTML(results ...string) string {
	var builder strings.Builder
	builder.WriteString("<html><body>")
	for _, res := range results {
		builder.WriteString(res)
	}
	builder.WriteString("</body></html>")
	return builder.String()
}

func newTestWebSearchTool(t *testing.T, handler http.HandlerFunc, opts *WebSearchOptions) *WebSearchTool {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	stubDuckDuckGoEndpoint(t, server.URL)

	var cfg WebSearchOptions
	if opts != nil {
		cfg = *opts
	}
	cfg.HTTPClient = server.Client()
	return NewWebSearchTool(&cfg)
}

func stubDuckDuckGoEndpoint(t *testing.T, url string) {
	t.Helper()
	prev := duckDuckGoEndpoint
	duckDuckGoEndpoint = url
	t.Cleanup(func() { duckDuckGoEndpoint = prev })
}
