package cli

import (

	"os"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
	"github.com/auto-code/auto-code/internal/commands/adddir"
	"github.com/auto-code/auto-code/internal/commands/agents"
	"github.com/auto-code/auto-code/internal/commands/clear"
	"github.com/auto-code/auto-code/internal/commands/commit"
	"github.com/auto-code/auto-code/internal/commands/compact"
	"github.com/auto-code/auto-code/internal/commands/config"
	"github.com/auto-code/auto-code/internal/commands/contextcmd"
	"github.com/auto-code/auto-code/internal/commands/cost"
	"github.com/auto-code/auto-code/internal/commands/diff"
	"github.com/auto-code/auto-code/internal/commands/doctor"
	"github.com/auto-code/auto-code/internal/commands/effort"
	"github.com/auto-code/auto-code/internal/commands/exit"
	"github.com/auto-code/auto-code/internal/commands/fast"
	"github.com/auto-code/auto-code/internal/commands/feedback"
	"github.com/auto-code/auto-code/internal/commands/files"
	"github.com/auto-code/auto-code/internal/commands/help"
	"github.com/auto-code/auto-code/internal/commands/hooks"
	"github.com/auto-code/auto-code/internal/commands/initcmd"
	"github.com/auto-code/auto-code/internal/commands/login"
	"github.com/auto-code/auto-code/internal/commands/logout"
	"github.com/auto-code/auto-code/internal/commands/mcp"
	"github.com/auto-code/auto-code/internal/commands/memory"
	"github.com/auto-code/auto-code/internal/commands/model"
	"github.com/auto-code/auto-code/internal/commands/permissions"
	"github.com/auto-code/auto-code/internal/commands/plan"
	"github.com/auto-code/auto-code/internal/commands/plugin"
	"github.com/auto-code/auto-code/internal/commands/resume"
	"github.com/auto-code/auto-code/internal/commands/review"
	"github.com/auto-code/auto-code/internal/commands/session"
	"github.com/auto-code/auto-code/internal/commands/share"
	"github.com/auto-code/auto-code/internal/commands/skills"
	"github.com/auto-code/auto-code/internal/commands/status"
	"github.com/auto-code/auto-code/internal/commands/tasks"
	"github.com/auto-code/auto-code/internal/commands/theme"
	"github.com/auto-code/auto-code/internal/commands/upgrade"
	"github.com/auto-code/auto-code/internal/commands/usage"
	"github.com/auto-code/auto-code/internal/commands/vim"
	"github.com/auto-code/auto-code/internal/state"
)

func NewDefaultCommandRegistry() *clitypes.CommandRegistry {
	r := clitypes.NewCommandRegistry()

	r.Register(help.NewHelpCommand(r))
	r.Register(clear.NewClearCommand())
	r.Register(exit.NewExitCommand())
	r.Register(initcmd.NewInitCommand())
	r.Register(resume.NewResumeCommand())
	r.Register(session.NewSessionCommand())
	r.Register(config.NewConfigCommand())
	r.Register(permissions.NewPermissionsCommand())
	r.Register(model.NewModelCommand())
	r.Register(login.NewLoginCommand())
	r.Register(logout.NewLogoutCommand())
	r.Register(plan.NewPlanCommand())
	r.Register(compact.NewCompactCommand())
	r.Register(commit.NewCommitCommand())
	r.Register(diff.NewDiffCommand())
	r.Register(review.NewReviewCommand())
	r.Register(tasks.NewTasksCommand())
	r.Register(mcp.NewMCPCommand())
	r.Register(skills.NewSkillsCommand())
	r.Register(plugin.NewPluginCommand())
	r.Register(hooks.NewHooksCommand())
	r.Register(memory.NewMemoryCommand())
	r.Register(vim.NewVimCommand())
	r.Register(agents.NewAgentsCommand())
	r.Register(adddir.NewAddDirCommand())
	r.Register(doctor.NewDoctorCommand())
	r.Register(status.NewStatusCommand())
	r.Register(cost.NewCostCommand())
	r.Register(usage.NewUsageCommand())
	r.Register(feedback.NewFeedbackCommand())
	r.Register(upgrade.NewUpgradeCommand())
	r.Register(contextcmd.NewContextCommand())
	r.Register(effort.NewEffortCommand())
	r.Register(fast.NewFastCommand())
	r.Register(files.NewFilesCommand())
	r.Register(share.NewShareCommand())
	r.Register(theme.NewThemeCommand())

	return r
}

func NewCommandContext(appState *state.AppState) *clitypes.CommandContext {
	cwd, _ := os.Getwd()
	return &clitypes.CommandContext{
		AppState: appState,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		CWD:      cwd,
	}
}

func IsCommandInput(input string) bool {
	return strings.HasPrefix(input, "/")
}
