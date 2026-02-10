package message

import "testing"

func TestCloneMessageDeepCopiesContentBlocks(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "text",
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockText, Text: "hello"},
			{Type: ContentBlockImage, MediaType: "image/png", Data: "base64"},
		},
	}
	cloned := CloneMessage(msg)

	if len(cloned.ContentBlocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(cloned.ContentBlocks))
	}
	if cloned.ContentBlocks[0].Text != "hello" {
		t.Fatalf("expected 'hello', got %q", cloned.ContentBlocks[0].Text)
	}

	// Mutate clone, verify original is unaffected
	cloned.ContentBlocks[0].Text = "modified"
	if msg.ContentBlocks[0].Text != "hello" {
		t.Fatalf("original mutated: %q", msg.ContentBlocks[0].Text)
	}
}

func TestCloneMessageNilContentBlocks(t *testing.T) {
	msg := Message{Role: "user", Content: "text"}
	cloned := CloneMessage(msg)
	if cloned.ContentBlocks != nil {
		t.Fatalf("expected nil ContentBlocks, got %v", cloned.ContentBlocks)
	}
}

func TestCloneMessageEmptyContentBlocks(t *testing.T) {
	msg := Message{Role: "user", ContentBlocks: []ContentBlock{}}
	cloned := CloneMessage(msg)
	if cloned.ContentBlocks != nil {
		t.Fatalf("expected nil for empty ContentBlocks, got %v", cloned.ContentBlocks)
	}
}

func TestNaiveCounterImageBlock(t *testing.T) {
	msg := Message{
		Role: "user",
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockImage, MediaType: "image/png", Data: "base64data"},
		},
	}
	got := (NaiveCounter{}).Count(msg)
	if got != 1600 {
		t.Fatalf("expected 1600 tokens for image block, got %d", got)
	}
}

func TestNaiveCounterDocumentBlock(t *testing.T) {
	// Data of length 600 → 600/6 + 500 = 600
	data := make([]byte, 600)
	for i := range data {
		data[i] = 'A'
	}
	msg := Message{
		Role: "user",
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockDocument, Data: string(data)},
		},
	}
	got := (NaiveCounter{}).Count(msg)
	want := 600/6 + 500
	if got != want {
		t.Fatalf("expected %d tokens for document block, got %d", want, got)
	}
}

func TestNaiveCounterMixedBlocks(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "abcd", // 4/4 = 1
		ContentBlocks: []ContentBlock{
			{Type: ContentBlockText, Text: "abcdefgh"}, // 8/4 = 2
			{Type: ContentBlockImage, Data: "x"},       // 1600
		},
	}
	got := (NaiveCounter{}).Count(msg)
	// Content: 4/4=1, Role: 4/10=0, text block: 8/4=2, image: 1600 → total 1603
	want := 1 + 2 + 1600
	if got != want {
		t.Fatalf("expected %d tokens, got %d", want, got)
	}
}
