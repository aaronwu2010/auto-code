package cli

import (
	"context"
	"io"

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