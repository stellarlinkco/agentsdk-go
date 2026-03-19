package api

import (
	"strings"

	"github.com/stellarlinkco/agentsdk-go/pkg/message"
)

func msgWithTokens(role string, tokens int) message.Message {
	if tokens <= 0 {
		return message.Message{Role: role, Content: ""}
	}
	content := strings.Repeat("x", tokens*4)
	msg := message.Message{Role: role, Content: content}
	counter := message.NaiveCounter{}
	for counter.Count(msg) < tokens {
		content += "xxxx"
		msg.Content = content
	}
	return msg
}
