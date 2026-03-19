package middleware

import (
	_ "embed"
	"html/template"
)

type traceTemplateData struct {
	SessionID, CreatedAt, UpdatedAt, JSONLog string
	EventCount, TotalTokens                  int
	TotalDuration                            int64
	EventsJSON                               template.JS
}

//go:embed trace_template.html
var traceHTMLTemplate string
