package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica project repo` is the CLI surface for project-scope repo bindings
// added in Step 2 of MUL-14. Members manage the workspace-scope set via the
// existing `multica workspace` settings UI; this command group is what makes
// project-scope binding usable from the terminal without the web app.
var projectRepoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage repos bound at project scope",
	Long: `Bind a git repo to a project so any agent assigned to an issue inside
the project can check it out. Bindings union with the workspace-scope set
when the daemon dispatches a task — workspace-only repos still appear, but
project-scope repos are visible only to issues that live inside the project.`,
}

var projectRepoListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List repos bound to a project",
	Args:  exactArgs(1),
	RunE:  runProjectRepoList,
}

var projectRepoAddCmd = &cobra.Command{
	Use:   "add <project-id> <url>",
	Short: "Bind a repo to a project",
	Args:  exactArgs(2),
	RunE:  runProjectRepoAdd,
}

var projectRepoRemoveCmd = &cobra.Command{
	Use:   "remove <project-id> <url-or-repo-id>",
	Short: "Unbind a repo from a project",
	Args:  exactArgs(2),
	RunE:  runProjectRepoRemove,
}

func init() {
	projectCmd.AddCommand(projectRepoCmd)
	projectRepoCmd.AddCommand(projectRepoListCmd)
	projectRepoCmd.AddCommand(projectRepoAddCmd)
	projectRepoCmd.AddCommand(projectRepoRemoveCmd)

	projectRepoListCmd.Flags().String("output", "table", "Output format: table or json")
	projectRepoAddCmd.Flags().String("description", "", "Optional description shown to agents")
	projectRepoAddCmd.Flags().String("output", "json", "Output format: table or json")
	projectRepoRemoveCmd.Flags().String("output", "table", "Output format: table or json")
}

// projectRepoPath builds the API path for a given project ID. Centralized
// here so the three subcommands can't drift on encoding rules.
func projectRepoPath(projectID string) string {
	return "/api/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + "/repos"
}

func runProjectRepoList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result struct {
		Repos []struct {
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"repos"`
	}
	if err := client.GetJSON(ctx, projectRepoPath(args[0]), &result); err != nil {
		return fmt.Errorf("list project repos: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		// JSON mode returns the raw array so callers piping into `jq` see
		// the same shape as `multica workspace get | .repos`.
		return cli.PrintJSON(os.Stdout, result.Repos)
	}

	if len(result.Repos) == 0 {
		fmt.Fprintln(os.Stderr, "No repos bound to this project.")
		return nil
	}
	headers := []string{"URL", "DESCRIPTION"}
	rows := make([][]string, 0, len(result.Repos))
	for _, r := range result.Repos {
		rows = append(rows, []string{r.URL, r.Description})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectRepoAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{
		"url":         strings.TrimSpace(args[1]),
		"description": "",
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, projectRepoPath(args[0]), body, &result); err != nil {
		return fmt.Errorf("add project repo: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stderr, "Bound %s to project %s.\n", args[1], truncateID(args[0]))
	return nil
}

func runProjectRepoRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// `args[1]` is either a UUID or a git URL. URLs contain `/` so they go on
	// the query string; UUIDs go on the path. The server accepts either.
	target := strings.TrimSpace(args[1])
	path := projectRepoPath(args[0])
	if looksLikeURL(target) {
		path += "?url=" + url.QueryEscape(target)
	} else {
		path += "/" + url.PathEscape(target)
	}

	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove project repo: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Unbound %s from project %s.\n", target, truncateID(args[0]))
	return nil
}

// looksLikeURL is the cheapest split between a UUID and a git URL. A UUID has
// exactly 36 chars with hyphens at fixed positions and no `/`; anything that
// isn't that shape is treated as a URL. The server falls back to URL matching
// if UUID parse fails, so a stray edge case here just costs one extra DB hit.
func looksLikeURL(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	if strings.Contains(s, ":") && !looksLikeUUID(s) {
		return true
	}
	return false
}

func looksLikeUUID(s string) bool {
	return len(s) == 36 &&
		s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
