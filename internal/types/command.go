package types

type Command struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	IsEnabled    bool   `json:"is_enabled"`
	Prompt       string `json:"prompt,omitempty"`
	DisableModel bool   `json:"disable_model,omitempty"`
}

type Plugin struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	IsEnabled   bool   `json:"is_enabled"`
	Commands    []Command `json:"commands,omitempty"`
}

type PluginError struct {
	PluginName string `json:"plugin_name"`
	Error      string `json:"error"`
}