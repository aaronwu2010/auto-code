package clitypes

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/auto-code/auto-code/internal/state"
)

type CommandResult struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

type CommandContext struct {
	AppState *state.AppState
	Args     []string
	Stdout   io.Writer
	Stderr   io.Writer
	CWD      string
}

type Command interface {
	Name() string
	Aliases() []string
	Description() string
	IsEnabled() bool
	Execute(ctx context.Context, cmdCtx *CommandContext) (*CommandResult, error)
}

type BaseCommand struct {
	CmdName        string
	CmdAliases     []string
	CmdDescription string
	CmdIsEnabled   bool
}

func NewBaseCommand(name, description string) *BaseCommand {
	return &BaseCommand{
		CmdName:        name,
		CmdAliases:     []string{},
		CmdDescription: description,
		CmdIsEnabled:   true,
	}
}

func (c *BaseCommand) Name() string        { return c.CmdName }
func (c *BaseCommand) Aliases() []string   { return c.CmdAliases }
func (c *BaseCommand) Description() string { return c.CmdDescription }
func (c *BaseCommand) IsEnabled() bool     { return c.CmdIsEnabled }

func (c *BaseCommand) Execute(_ context.Context, _ *CommandContext) (*CommandResult, error) {
	return &CommandResult{Output: "not implemented"}, nil
}

type CommandRegistry struct {
	commands map[string]Command
}

func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		commands: make(map[string]Command),
	}
}

func (r *CommandRegistry) Register(cmd Command) {
	r.commands[cmd.Name()] = cmd
	for _, alias := range cmd.Aliases() {
		r.commands[alias] = cmd
	}
}

func (r *CommandRegistry) Get(name string) (Command, bool) {
	cmd, ok := r.commands[name]
	return cmd, ok
}

func (r *CommandRegistry) All() []Command {
	seen := make(map[string]bool)
	var result []Command
	for _, cmd := range r.commands {
		if !seen[cmd.Name()] {
			seen[cmd.Name()] = true
			result = append(result, cmd)
		}
	}
	return result
}

func (r *CommandRegistry) Execute(ctx context.Context, input string, cmdCtx *CommandContext) (*CommandResult, error) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	name := strings.TrimPrefix(parts[0], "/")
	args := parts[1:]

	cmd, ok := r.Get(name)
	if !ok {
		return &CommandResult{Error: fmt.Sprintf("Unknown command: /%s. Type /help for available commands.", name)}, nil
	}

	if !cmd.IsEnabled() {
		return &CommandResult{Error: fmt.Sprintf("Command /%s is currently disabled.", name)}, nil
	}

	cmdCtx.Args = args
	return cmd.Execute(ctx, cmdCtx)
}

func FormatCommandList(commands []Command) string {
	var sb strings.Builder
	maxNameLen := 0
	for _, cmd := range commands {
		if len(cmd.Name()) > maxNameLen {
			maxNameLen = len(cmd.Name())
		}
	}
	for _, cmd := range commands {
		name := cmd.Name()
		padding := strings.Repeat(" ", maxNameLen-len(name)+2)
		sb.WriteString(fmt.Sprintf("  /%s%s%s\n", name, padding, cmd.Description()))
	}
	return sb.String()
}

func SortCommands(commands []Command) []Command {
	sorted := make([]Command, len(commands))
	copy(sorted, commands)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name() < sorted[j].Name()
	})
	return sorted
}