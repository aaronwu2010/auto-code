package types

import "testing"

func TestSystemPrompt_BuildContentFromBlocks(t *testing.T) {
	sp := &SystemPrompt{
		Blocks: []SystemPromptBlock{
			{Text: "static part", CacheScope: "global"},
			{Text: "dynamic part", CacheScope: ""},
		},
	}
	got := sp.BuildContent()
	want := "static part\n\ndynamic part"
	if got != want {
		t.Errorf("BuildContent() = %q, want %q", got, want)
	}
}

func TestSystemPrompt_BuildContentPrefersExplicitContent(t *testing.T) {
	sp := &SystemPrompt{
		Content: "explicit",
		Blocks:  []SystemPromptBlock{{Text: "from blocks"}},
	}
	if got := sp.BuildContent(); got != "explicit" {
		t.Errorf("BuildContent() = %q, want %q", got, "explicit")
	}
}

func TestSystemPrompt_BuildContentEmpty(t *testing.T) {
	sp := &SystemPrompt{}
	if got := sp.BuildContent(); got != "" {
		t.Errorf("BuildContent() = %q, want empty", got)
	}
}
