package registry

import (
	"sort"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/tools/agent"
	"github.com/auto-code/auto-code/internal/tools/ask"
	"github.com/auto-code/auto-code/internal/tools/bash"
	"github.com/auto-code/auto-code/internal/tools/brief"
	"github.com/auto-code/auto-code/internal/tools/config"
	"github.com/auto-code/auto-code/internal/tools/cron"
	"github.com/auto-code/auto-code/internal/tools/fileedit"
	"github.com/auto-code/auto-code/internal/tools/fileread"
	"github.com/auto-code/auto-code/internal/tools/filewrite"
	"github.com/auto-code/auto-code/internal/tools/glob"
	"github.com/auto-code/auto-code/internal/tools/grep"
	"github.com/auto-code/auto-code/internal/tools/lsp"
	"github.com/auto-code/auto-code/internal/tools/mcp"
	"github.com/auto-code/auto-code/internal/tools/mcpauth"
	"github.com/auto-code/auto-code/internal/tools/mcpresource"
	"github.com/auto-code/auto-code/internal/tools/message"
	"github.com/auto-code/auto-code/internal/tools/monitor"
	"github.com/auto-code/auto-code/internal/tools/notebook"
	"github.com/auto-code/auto-code/internal/tools/planmode"
	"github.com/auto-code/auto-code/internal/tools/powershell"
	"github.com/auto-code/auto-code/internal/tools/repl"
	"github.com/auto-code/auto-code/internal/tools/skill"
	"github.com/auto-code/auto-code/internal/tools/sleep"
	"github.com/auto-code/auto-code/internal/tools/snip"
	"github.com/auto-code/auto-code/internal/tools/synthetic"
	"github.com/auto-code/auto-code/internal/tools/task"
	"github.com/auto-code/auto-code/internal/tools/team"
	"github.com/auto-code/auto-code/internal/tools/todo"
	"github.com/auto-code/auto-code/internal/tools/toosearch"
	"github.com/auto-code/auto-code/internal/tools/webbrowser"
	"github.com/auto-code/auto-code/internal/tools/webfetch"
	"github.com/auto-code/auto-code/internal/tools/websearch"
	"github.com/auto-code/auto-code/internal/tools/worktree"
	"github.com/auto-code/auto-code/internal/types"
)

type ToolRegistry struct {
	baseTools []tools.Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		baseTools: make([]tools.Tool, 0),
	}
}

func NewDefaultToolRegistry() *ToolRegistry {
	r := NewToolRegistry()
	r.Register(fileread.NewFileReadTool())
	r.Register(fileedit.NewFileEditTool())
	r.Register(filewrite.NewFileWriteTool())
	r.Register(glob.NewGlobTool())
	r.Register(grep.NewGrepTool())
	r.Register(websearch.NewWebSearchTool())
	r.Register(webfetch.NewWebFetchTool())
	r.Register(bash.NewBashTool())
	r.Register(powershell.NewPowerShellTool())
	r.Register(repl.NewREPLTool())
	r.Register(task.NewTaskCreateTool())
	r.Register(task.NewTaskGetTool())
	r.Register(task.NewTaskListTool())
	r.Register(task.NewTaskOutputTool())
	r.Register(task.NewTaskStopTool())
	r.Register(task.NewTaskUpdateTool())
	r.Register(team.NewTeamCreateTool())
	r.Register(team.NewTeamDeleteTool())
	r.Register(ask.NewAskTool())
	r.Register(brief.NewBriefTool())
	r.Register(config.NewConfigTool())
	r.Register(cron.NewCronTool())
	r.Register(lsp.NewLSPTool())
	r.Register(notebook.NewNotebookEditTool())
	r.Register(message.NewMessageTool())
	r.Register(skill.NewSkillTool())
	r.Register(sleep.NewSleepTool())
	r.Register(todo.NewTodoWriteTool())
	r.Register(mcp.NewMCPTool())
	r.Register(mcpauth.NewMcpAuthTool())
	r.Register(mcpresource.NewMcpResourceTool())
	r.Register(planmode.NewEnterPlanModeTool())
	r.Register(planmode.NewExitPlanModeTool())
	r.Register(worktree.NewEnterWorktreeTool())
	r.Register(worktree.NewExitWorktreeTool())
	r.Register(monitor.NewMonitorTool())
	r.Register(snip.NewSnipTool())
	r.Register(synthetic.NewSyntheticTool())
	r.Register(toosearch.NewToolSearchTool())
	r.Register(webbrowser.NewWebBrowserTool())
	r.Register(agent.NewAgentTool())
	return r
}

func (r *ToolRegistry) Register(tool tools.Tool) {
	r.baseTools = append(r.baseTools, tool)
}

func (r *ToolRegistry) GetAllBaseTools() []tools.Tool {
	return r.baseTools
}

func (r *ToolRegistry) GetTools(permissionCtx types.ToolPermissionContext) []tools.Tool {
	result := make([]tools.Tool, 0)
	for _, t := range r.baseTools {
		if !t.IsEnabled() {
			continue
		}
		if isDeniedByRules(t, permissionCtx.AlwaysDenyRules) {
			continue
		}
		result = append(result, t)
	}
	return result
}

func (r *ToolRegistry) AssembleToolPool(permissionCtx types.ToolPermissionContext, mcpTools []tools.Tool) []tools.Tool {
	builtinTools := r.GetTools(permissionCtx)

	allTools := make([]tools.Tool, 0, len(builtinTools)+len(mcpTools))
	allTools = append(allTools, builtinTools...)
	allTools = append(allTools, mcpTools...)

	sort.Slice(allTools, func(i, j int) bool {
		return allTools[i].Name() < allTools[j].Name()
	})

	seen := make(map[string]bool)
	unique := make([]tools.Tool, 0, len(allTools))
	for _, t := range allTools {
		if !seen[t.Name()] {
			seen[t.Name()] = true
			unique = append(unique, t)
		}
	}

	return unique
}

func isDeniedByRules(tool tools.Tool, rules types.ToolPermissionRulesBySource) bool {
	for _, ruleList := range rules {
		for _, rule := range ruleList {
			if tools.ToolMatchesName(tool, rule.ToolName) {
				return true
			}
		}
	}
	return false
}