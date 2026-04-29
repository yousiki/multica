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

// `multica issue repo` is the CLI surface for issue-scope repo bindings added
// in Step 3 of MUL-14. The server endpoints resolve the issue from `<id>`
// using the same `loadIssueForUser` loader the rest of `multica issue` uses,
// so callers can pass either a UUID or the `MUL-123` identifier form here.
var issueRepoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage repos bound at issue scope",
	Long: `Bind a git repo to a single issue. The agent claiming a task on this
issue receives workspace ∪ project ∪ issue bindings (deduped) as its
operational repo set, with issue-scope descriptions winning on URL
collision. Useful for one-shot scope extensions — a feature that touches a
repo not normally part of the project's scope, for example.`,
}

var issueRepoListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List repos bound to an issue",
	Args:  exactArgs(1),
	RunE:  runIssueRepoList,
}

var issueRepoAddCmd = &cobra.Command{
	Use:   "add <issue-id> <url>",
	Short: "Bind a repo to an issue",
	Args:  exactArgs(2),
	RunE:  runIssueRepoAdd,
}

var issueRepoRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id> <url-or-repo-id>",
	Short: "Unbind a repo from an issue",
	Args:  exactArgs(2),
	RunE:  runIssueRepoRemove,
}

func init() {
	issueCmd.AddCommand(issueRepoCmd)
	issueRepoCmd.AddCommand(issueRepoListCmd)
	issueRepoCmd.AddCommand(issueRepoAddCmd)
	issueRepoCmd.AddCommand(issueRepoRemoveCmd)

	issueRepoListCmd.Flags().String("output", "table", "Output format: table or json")
	issueRepoAddCmd.Flags().String("description", "", "Optional description shown to agents")
	issueRepoAddCmd.Flags().String("output", "json", "Output format: table or json")
	issueRepoRemoveCmd.Flags().String("output", "table", "Output format: table or json")
}

// issueRepoPath builds the API path for a given issue ID. Centralized so the
// three subcommands can't drift on encoding rules. The issue identifier is
// URL-path-escaped: the `MUL-123` form has no problematic characters but the
// path-escape costs nothing and keeps the rule uniform with the project
// counterpart.
func issueRepoPath(issueID string) string {
	return "/api/issues/" + url.PathEscape(strings.TrimSpace(issueID)) + "/repos"
}

func runIssueRepoList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueID := strings.TrimSpace(args[0])
	if issueID == "" {
		return fmt.Errorf("issue id is required")
	}

	var result struct {
		Repos []struct {
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"repos"`
	}
	if err := client.GetJSON(ctx, issueRepoPath(issueID), &result); err != nil {
		return fmt.Errorf("list issue repos: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		// Match `multica project repo list --output json` — emit the bare
		// array so `jq` users see the same shape across both commands.
		return cli.PrintJSON(os.Stdout, result.Repos)
	}

	if len(result.Repos) == 0 {
		fmt.Fprintln(os.Stderr, "No repos bound to this issue.")
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

func runIssueRepoAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueID := strings.TrimSpace(args[0])
	if issueID == "" {
		return fmt.Errorf("issue id is required")
	}

	body := map[string]any{
		"url":         strings.TrimSpace(args[1]),
		"description": "",
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, issueRepoPath(issueID), body, &result); err != nil {
		return fmt.Errorf("add issue repo: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stderr, "Bound %s to issue %s.\n", args[1], issueID)
	return nil
}

func runIssueRepoRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueID := strings.TrimSpace(args[0])
	if issueID == "" {
		return fmt.Errorf("issue id is required")
	}

	// `args[1]` is either a UUID or a git URL. URLs contain `/` so they go on
	// the query string; UUIDs go on the path. The server accepts either.
	target := strings.TrimSpace(args[1])
	path := issueRepoPath(issueID)
	if looksLikeURL(target) {
		path += "?url=" + url.QueryEscape(target)
	} else {
		path += "/" + url.PathEscape(target)
	}

	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove issue repo: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Unbound %s from issue %s.\n", target, issueID)
	return nil
}
