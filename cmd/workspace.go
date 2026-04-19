package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/aidan-bailey/loom/config"
	"github.com/aidan-bailey/loom/session/git"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var workspaceName string

// WorkspaceCmd is the parent command for workspace management.
var WorkspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage workspaces",
}

var workspaceAddCmd = &cobra.Command{
	Use:   "add [path]",
	Short: "Register a git repository as a workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}

		if !git.IsGitRepo(absPath, Exec{}) {
			return fmt.Errorf("%s is not a git repository", absPath)
		}

		name := workspaceName
		if name == "" {
			name = filepath.Base(absPath)
		}

		reg, err := config.LoadWorkspaceRegistry()
		if err != nil {
			return err
		}

		if err := reg.Add(name, absPath); err != nil {
			return err
		}

		fmt.Printf("Workspace %q added at %s\n", name, absPath)
		return nil
	},
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered workspaces",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := config.LoadWorkspaceRegistry()
		if err != nil {
			return err
		}

		if len(reg.Workspaces) == 0 {
			fmt.Println("No workspaces registered. Use 'loom workspace add' to register one.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPATH\tSTATUS")
		for _, ws := range reg.Workspaces {
			status := ""
			if ws.Name == reg.LastUsed {
				status = "[last used]"
			}
			if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
				status = "[missing]"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", ws.Name, ws.Path, status)
		}
		return w.Flush()
	},
}

var workspaceRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Unregister a workspace (does not delete data)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := config.LoadWorkspaceRegistry()
		if err != nil {
			return err
		}

		if err := reg.Remove(args[0]); err != nil {
			return err
		}

		fmt.Printf("Workspace %q removed\n", args[0])
		return nil
	},
}

// instanceSummary is a minimal struct for reading instance JSON, shared
// by the migrate and status commands — only the fields they access are decoded.
type instanceSummary struct {
	Title    string `json:"title"`
	Worktree struct {
		RepoPath     string `json:"repo_path"`
		WorktreePath string `json:"worktree_path"`
	} `json:"worktree"`
}

// mergeWorkspaceInstances merges newInstances into a workspace's existing
// instance data. Duplicates (by Title) are skipped. Returns the merged JSON,
// the count of entries appended, and an error if existingData is present but
// cannot be parsed as a JSON array.
func mergeWorkspaceInstances(existingData json.RawMessage, newInstances []json.RawMessage) ([]byte, int, error) {
	var existingRaw []json.RawMessage
	if existingData != nil && string(existingData) != "[]" {
		if err := json.Unmarshal(existingData, &existingRaw); err != nil {
			return nil, 0, fmt.Errorf("failed to parse existing workspace instances: %w", err)
		}
	}

	existingTitles := make(map[string]bool)
	for _, raw := range existingRaw {
		var inst instanceSummary
		if err := json.Unmarshal(raw, &inst); err != nil {
			continue
		}
		existingTitles[inst.Title] = true
	}

	added := 0
	for _, raw := range newInstances {
		var inst instanceSummary
		if err := json.Unmarshal(raw, &inst); err != nil {
			continue
		}
		if existingTitles[inst.Title] {
			continue
		}
		existingRaw = append(existingRaw, raw)
		existingTitles[inst.Title] = true
		added++
	}

	merged, err := json.Marshal(existingRaw)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal merged instances: %w", err)
	}
	return merged, added, nil
}

var workspaceUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Set the default workspace for future invocations",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := config.LoadWorkspaceRegistry()
		if err != nil {
			return err
		}
		if err := reg.UpdateLastUsed(args[0]); err != nil {
			return err
		}
		fmt.Printf("Default workspace set to %q\n", args[0])
		return nil
	},
}

var workspaceRenameCmd = &cobra.Command{
	Use:   "rename <old-name> <new-name>",
	Short: "Rename a workspace",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := config.LoadWorkspaceRegistry()
		if err != nil {
			return err
		}

		if err := reg.Rename(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("Workspace %q renamed to %q\n", args[0], args[1])
		return nil
	},
}

var workspaceStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Show instance counts for a workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := config.LoadWorkspaceRegistry()
		if err != nil {
			return err
		}

		var ws *config.Workspace
		if len(args) > 0 {
			ws = reg.Get(args[0])
			if ws == nil {
				return fmt.Errorf("workspace %q not found", args[0])
			}
		} else {
			// Auto-detect from cwd
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}
			ws = reg.FindByPath(cwd)
			if ws == nil {
				return fmt.Errorf("no workspace matches current directory; specify a name")
			}
		}

		wsDir := config.WorkspaceConfigDir(ws)
		state := config.LoadStateFrom(wsDir)
		if state.InstancesData == nil || string(state.InstancesData) == "[]" {
			fmt.Printf("Workspace %q (%s): 0 instances\n", ws.Name, ws.Path)
			return nil
		}

		var instances []instanceSummary
		if err := json.Unmarshal(state.InstancesData, &instances); err != nil {
			return fmt.Errorf("failed to parse instances: %w", err)
		}

		fmt.Printf("Workspace %q (%s): %d instance(s)\n", ws.Name, ws.Path, len(instances))
		for _, inst := range instances {
			fmt.Printf("  - %s\n", inst.Title)
		}
		return nil
	},
}

func init() {
	workspaceAddCmd.Flags().StringVar(&workspaceName, "name", "", "Override workspace name (defaults to directory basename)")
	WorkspaceCmd.AddCommand(workspaceAddCmd)
	WorkspaceCmd.AddCommand(workspaceListCmd)
	WorkspaceCmd.AddCommand(workspaceRemoveCmd)
	WorkspaceCmd.AddCommand(workspaceMigrateCmd)
	WorkspaceCmd.AddCommand(workspaceUseCmd)
	WorkspaceCmd.AddCommand(workspaceRenameCmd)
	WorkspaceCmd.AddCommand(workspaceStatusCmd)
}
