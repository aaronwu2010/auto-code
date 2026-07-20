package model

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/clitypes"
	"github.com/auto-code/auto-code/internal/types"
)

type ModelCommand struct{ *clitypes.BaseCommand }

func NewModelCommand() *ModelCommand {
	return &ModelCommand{BaseCommand: clitypes.NewBaseCommand("model", "Select or change the AI model")}
}

func (c *ModelCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		var currentModel types.ModelSetting
		if cmdCtx.AppState != nil {
			currentModel = cmdCtx.AppState.GetMainLoopModel()
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Current model: %s\nUsage: /model <model-name>", currentModel)}, nil
	}

	modelName := cmdCtx.Args[0]
	if cmdCtx.AppState != nil {
		cmdCtx.AppState.SetMainLoopModel(types.ModelSetting(modelName))
	}
	return &clitypes.CommandResult{Output: fmt.Sprintf("Model set to: %s", modelName)}, nil
}
