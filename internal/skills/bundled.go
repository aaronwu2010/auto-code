package skills


type BundledSkillDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	IsEnabled   bool   `json:"isEnabled"`
	DisableModel bool  `json:"disableModel,omitempty"`
	Type        string `json:"type"`
}

var bundledSkills map[string]*BundledSkillDefinition

func init() {
	bundledSkills = make(map[string]*BundledSkillDefinition)
}

func RegisterBundledSkill(def BundledSkillDefinition) {
	bundledSkills[def.ID] = &def
}

func GetBundledSkills() []*BundledSkillDefinition {
	result := make([]*BundledSkillDefinition, 0, len(bundledSkills))
	for _, s := range bundledSkills {
		result = append(result, s)
	}
	return result
}

func ClearBundledSkills() {
	bundledSkills = make(map[string]*BundledSkillDefinition)
}

func GetBundledSkillExtractDir() string {
	return ""
}

func InitBundledSkills() {
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "commit", Name: "commit",
		Description: "Generate a git commit message",
		Prompt:      "Analyze the staged changes and generate a commit message.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "review", Name: "review",
		Description: "Review code changes",
		Prompt:      "Review the code changes and provide feedback.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "init", Name: "init",
		Description: "Initialize a new project",
		Prompt:      "Initialize the project with appropriate configuration files.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "debug", Name: "debug",
		Description: "Debug an issue",
		Prompt:      "Help debug the issue by analyzing error messages and code.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "simplify", Name: "simplify",
		Description: "Simplify code",
		Prompt:      "Simplify the code while preserving functionality.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "verify", Name: "verify",
		Description: "Verify a condition",
		Prompt:      "Verify that the specified condition is met.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "remember", Name: "remember",
		Description: "Remember important information",
		Prompt:      "Store this information for future reference.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "stuck", Name: "stuck",
		Description: "Help when stuck",
		Prompt:      "I'm stuck. Help me think through this problem step by step.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "loop", Name: "loop",
		Description: "Loop over items",
		Prompt:      "Iterate over the specified items and perform the action.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "batch", Name: "batch",
		Description: "Batch process files",
		Prompt:      "Process multiple files in batch.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "keybindings", Name: "keybindings",
		Description: "Manage keybindings",
		Prompt:      "Show and manage keyboard shortcuts.",
		IsEnabled:   true, Type: "prompt",
	})
	RegisterBundledSkill(BundledSkillDefinition{
		ID: "update-config", Name: "update-config",
		Description: "Update configuration",
		Prompt:      "Update the project or user configuration.",
		IsEnabled:   true, Type: "prompt",
	})
}