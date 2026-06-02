package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/teams"
)

func newTeamAuditCmd() *cobra.Command {
	var (
		limit      int
		actorEmail string
	)

	cmd := &cobra.Command{
		Use:   "audit [team-id]",
		Short: "View team audit log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]

			if actorEmail == "" {
				return fmt.Errorf("actor email required (use --as)")
			}

			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			team, err := findTeam(service, teamID)
			if err != nil {
				return err
			}

			actor, err := service.GetMemberByEmail(team.ID, actorEmail)
			if err != nil || actor == nil {
				return fmt.Errorf("you are not a member of this team")
			}

			entries, err := service.GetAuditLog(team.ID, actor.ID, limit)
			if err != nil {
				return fmt.Errorf("failed to get audit log: %w", err)
			}

			if len(entries) == 0 {
				fmt.Println("No audit entries found.")
				return nil
			}

			fmt.Printf("📜 Audit Log for '%s' (showing %d entries)\n", team.Name, len(entries))
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

			for _, e := range entries {
				fmt.Printf("[%s] %s performed %s on %s",
					e.CreatedAt.Format("2006-01-02 15:04"),
					e.ActorEmail,
					e.Action,
					e.Resource,
				)
				if e.ResourceID != "" {
					fmt.Printf(" (%s)", e.ResourceID[:8])
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum entries to show")
	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have view_audit_log permission)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

func newTeamProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage team project access",
	}

	cmd.AddCommand(
		newTeamProjectSetCmd(),
		newTeamProjectRemoveCmd(),
		newTeamProjectListCmd(),
	)

	return cmd
}

func newTeamProjectSetCmd() *cobra.Command {
	var (
		defaultRole string
		actorEmail  string
	)

	cmd := &cobra.Command{
		Use:   "set [team-id] [project-path]",
		Short: "Set project access with a default role",
		Long: `Set or update project access for a team with a default role.

The default role determines the minimum permission level for all team members
on the specified project. Members may still have higher permissions based on
their individual role.

Example:
  pilot team project set abc123 /path/to/project --role developer --as owner@example.com`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]
			projectPath := args[1]

			if actorEmail == "" {
				return fmt.Errorf("actor email required (use --as)")
			}

			if !teams.Role(defaultRole).IsValid() {
				return fmt.Errorf("invalid role: %s (use owner, admin, developer, or viewer)", defaultRole)
			}

			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			team, err := findTeam(service, teamID)
			if err != nil {
				return err
			}

			actor, err := service.GetMemberByEmail(team.ID, actorEmail)
			if err != nil || actor == nil {
				return fmt.Errorf("you are not a member of this team")
			}

			if err := service.SetProjectAccess(team.ID, actor.ID, projectPath, teams.Role(defaultRole)); err != nil {
				return fmt.Errorf("failed to set project access: %w", err)
			}

			fmt.Printf("✅ Set project access for '%s' on team '%s' (default role: %s)\n", projectPath, team.Name, defaultRole)
			return nil
		},
	}

	cmd.Flags().StringVar(&defaultRole, "role", "developer", "Default role: owner, admin, developer, viewer")
	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have manage_projects permission)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

func newTeamProjectRemoveCmd() *cobra.Command {
	var actorEmail string

	cmd := &cobra.Command{
		Use:   "remove [team-id] [project-path]",
		Short: "Remove project access from a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]
			projectPath := args[1]

			if actorEmail == "" {
				return fmt.Errorf("actor email required (use --as)")
			}

			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			team, err := findTeam(service, teamID)
			if err != nil {
				return err
			}

			actor, err := service.GetMemberByEmail(team.ID, actorEmail)
			if err != nil || actor == nil {
				return fmt.Errorf("you are not a member of this team")
			}

			if err := service.RemoveProjectAccess(team.ID, actor.ID, projectPath); err != nil {
				return fmt.Errorf("failed to remove project access: %w", err)
			}

			fmt.Printf("✅ Removed project access for '%s' from team '%s'\n", projectPath, team.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have manage_projects permission)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

func newTeamProjectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [team-id]",
		Short: "List project access entries for a team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]

			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			team, err := findTeam(service, teamID)
			if err != nil {
				return err
			}

			accesses, err := service.ListProjectAccess(team.ID)
			if err != nil {
				return fmt.Errorf("failed to list project access: %w", err)
			}

			if len(accesses) == 0 {
				fmt.Printf("No project access entries for team '%s'.\n", team.Name)
				fmt.Println()
				fmt.Println("Add one with:")
				fmt.Printf("   pilot team project set %s /path/to/project --role developer --as you@example.com\n", team.ID[:8])
				return nil
			}

			fmt.Printf("📂 Project Access for '%s' (%d entries):\n\n", team.Name, len(accesses))

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "PROJECT PATH\tDEFAULT ROLE")
			_, _ = fmt.Fprintln(w, "────────────\t────────────")

			for _, a := range accesses {
				_, _ = fmt.Fprintf(w, "%s\t%s\n", a.ProjectPath, a.DefaultRole)
			}
			_ = w.Flush()

			return nil
		},
	}
}
