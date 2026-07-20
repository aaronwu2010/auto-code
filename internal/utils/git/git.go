package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func GetBranch(ctx context.Context, cwd string) (string, error) {
	out, err := runGitCommand(ctx, cwd, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func GetDefaultBranch(ctx context.Context, cwd string) (string, error) {
	out, err := runGitCommand(ctx, cwd, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "main", nil
	}
	parts := strings.Split(strings.TrimSpace(out), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1], nil
	}
	return "main", nil
}

func GetIsGit(ctx context.Context, cwd string) (bool, error) {
	_, err := runGitCommand(ctx, cwd, "rev-parse", "--git-dir")
	return err == nil, nil
}

func GetStatus(ctx context.Context, cwd string) (string, error) {
	out, err := runGitCommand(ctx, cwd, "status", "--short")
	if err != nil {
		return "", err
	}
	result := strings.TrimSpace(out)
	if len(result) > 2000 {
		result = result[:2000] + "\n... (truncated)"
	}
	return result, nil
}

func GetRecentCommits(ctx context.Context, cwd string, n int) (string, error) {
	out, err := runGitCommand(ctx, cwd, "log", "--oneline", fmt.Sprintf("-%d", n))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func GetUserName(ctx context.Context, cwd string) (string, error) {
	out, err := runGitCommand(ctx, cwd, "config", "user.name")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

func GetDiff(ctx context.Context, cwd string, args ...string) (string, error) {
	allArgs := append([]string{"diff"}, args...)
	out, err := runGitCommand(ctx, cwd, allArgs...)
	if err != nil {
		return "", err
	}
	return out, nil
}

func Commit(ctx context.Context, cwd string, message string) error {
	_, err := runGitCommand(ctx, cwd, "commit", "-m", message)
	return err
}

func runGitCommand(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}