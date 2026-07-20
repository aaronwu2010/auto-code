package tasks

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type TasksCommand struct{ *clitypes.BaseCommand }

func NewTasksCommand() *TasksCommand {
	return &TasksCommand{BaseCommand: clitypes.NewBaseCommand("tasks", "Manage background tasks")}
}

func (c *TasksCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Task management:
  /tasks list       - List all tasks
  /tasks stop <id>  - Stop a running task
  /tasks output <id>- Get task output`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Tasks:\n  (no active tasks)"}, nil
	case "stop":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /tasks stop <id>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Task %s stopped.", cmdCtx.Args[1])}, nil
	case "output":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /tasks output <id>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Output for task %s: (no output yet)", cmdCtx.Args[1])}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
