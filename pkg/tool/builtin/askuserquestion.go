package toolbuiltin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cexll/agentsdk-go/pkg/tool"
)

const askUserQuestionDescription = `Use this tool when you need to ask the user questions during execution. This allows you to:
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices as you work
4. Offer choices to the user about what direction to take.

Usage notes:
- Users will always be able to select "Other" to provide custom text input
- Use multiSelect: true to allow multiple answers to be selected for a question
`

// QuestionOption represents a selectable option for a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Question represents a question to ask the user.
type Question struct {
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect"`
}

// AskUserQuestionTool requests user input via an external permission component.
type AskUserQuestionTool struct{}

func NewAskUserQuestionTool() *AskUserQuestionTool { return &AskUserQuestionTool{} }

func (t *AskUserQuestionTool) Name() string { return "AskUserQuestion" }

func (t *AskUserQuestionTool) Description() string { return askUserQuestionDescription }

func (t *AskUserQuestionTool) Schema() *tool.JSONSchema { return askUserQuestionSchema }

func (t *AskUserQuestionTool) Execute(ctx context.Context, params map[string]interface{}) (*tool.ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	questions, answers, err := parseAskUserQuestionParams(params)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data := map[string]interface{}{
		"questions": questions,
	}
	if len(answers) > 0 {
		data["answers"] = answers
	}

	return &tool.ToolResult{
		Success: true,
		Output:  formatAskUserQuestionOutput(questions),
		Data:    data,
	}, nil
}

func parseAskUserQuestionParams(params map[string]interface{}) ([]Question, map[string]string, error) {
	if params == nil {
		return nil, nil, errors.New("params is nil")
	}
	rawQuestions, ok := params["questions"]
	if !ok {
		return nil, nil, errors.New("questions is required")
	}
	questionsList, err := coerceInterfaceArray(rawQuestions, "questions")
	if err != nil {
		return nil, nil, err
	}

	questions := make([]Question, len(questionsList))
	for i, raw := range questionsList {
		obj, ok := raw.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("questions[%d] must be object, got %T", i, raw)
		}
		q, err := parseQuestion(i, obj)
		if err != nil {
			return nil, nil, err
		}
		questions[i] = q
	}

	answers, err := parseOptionalAnswers(params)
	if err != nil {
		return nil, nil, err
	}
	return questions, answers, nil
}

func parseQuestion(idx int, obj map[string]interface{}) (Question, error) {
	question, err := readRequiredString(obj, "question")
	if err != nil {
		return Question{}, fmt.Errorf("questions[%d].question: %w", idx, err)
	}
	header, err := readRequiredString(obj, "header")
	if err != nil {
		return Question{}, fmt.Errorf("questions[%d].header: %w", idx, err)
	}
	optionsRaw, ok := obj["options"]
	if !ok {
		return Question{}, fmt.Errorf("questions[%d].options: field is required", idx)
	}
	optionsList, err := coerceInterfaceArray(optionsRaw, fmt.Sprintf("questions[%d].options", idx))
	if err != nil {
		return Question{}, err
	}
	options := make([]QuestionOption, len(optionsList))
	for j, raw := range optionsList {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return Question{}, fmt.Errorf("questions[%d].options[%d] must be object, got %T", idx, j, raw)
		}
		opt, err := parseOption(idx, j, entry)
		if err != nil {
			return Question{}, err
		}
		options[j] = opt
	}

	rawMulti, ok := obj["multiSelect"]
	if !ok {
		return Question{}, fmt.Errorf("questions[%d].multiSelect: field is required", idx)
	}
	multi, ok := rawMulti.(bool)
	if !ok {
		return Question{}, fmt.Errorf("questions[%d].multiSelect must be boolean, got %T", idx, rawMulti)
	}
	return Question{
		Question:    question,
		Header:      header,
		Options:     options,
		MultiSelect: multi,
	}, nil
}

func parseOption(qIdx, oIdx int, obj map[string]interface{}) (QuestionOption, error) {
	label, err := readRequiredString(obj, "label")
	if err != nil {
		return QuestionOption{}, fmt.Errorf("questions[%d].options[%d].label: %w", qIdx, oIdx, err)
	}
	desc, err := readRequiredString(obj, "description")
	if err != nil {
		return QuestionOption{}, fmt.Errorf("questions[%d].options[%d].description: %w", qIdx, oIdx, err)
	}
	return QuestionOption{Label: label, Description: desc}, nil
}

func parseOptionalAnswers(params map[string]interface{}) (map[string]string, error) {
	raw, ok := params["answers"]
	if !ok || raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case map[string]string:
		if len(v) == 0 {
			return nil, nil
		}
		out := make(map[string]string, len(v))
		for key, value := range v {
			out[key] = strings.TrimSpace(value)
		}
		return out, nil
	case map[string]interface{}:
		if len(v) == 0 {
			return nil, nil
		}
		out := make(map[string]string, len(v))
		for key, value := range v {
			s, err := coerceString(value)
			if err != nil {
				return nil, fmt.Errorf("answers[%q] must be string: %w", key, err)
			}
			out[key] = strings.TrimSpace(s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("answers must be object, got %T", raw)
	}
}

func coerceInterfaceArray(value interface{}, field string) ([]interface{}, error) {
	switch v := value.(type) {
	case []interface{}:
		return v, nil
	case []map[string]interface{}:
		out := make([]interface{}, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an array, got %T", field, value)
	}
}

func formatAskUserQuestionOutput(questions []Question) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d question(s)\n", len(questions))
	for i, q := range questions {
		mode := "single-select"
		if q.MultiSelect {
			mode = "multi-select"
		}
		fmt.Fprintf(&b, "%d. [%s] %s (%s)\n", i+1, q.Header, q.Question, mode)
		for j, opt := range q.Options {
			fmt.Fprintf(&b, "   %d) %s - %s\n", j+1, opt.Label, opt.Description)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func readRequiredString(obj map[string]interface{}, key string) (string, error) {
	raw, ok := obj[key]
	if !ok {
		return "", errors.New("field is required")
	}
	value, err := coerceString(raw)
	if err != nil {
		return "", fmt.Errorf("must be string: %w", err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("cannot be empty")
	}
	return value, nil
}
