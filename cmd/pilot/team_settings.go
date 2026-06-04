package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/teams"
)

// newTeamSettingsCmd surfaces Service.UpdateTeamSettings on the CLI.
func newTeamSettingsCmd() *cobra.Command {
	var (
		actorEmail         string
		maxConcurrentTasks int
		defaultBranch      string
		allowedProjects    []string
	)

	cmd := &cobra.Command{
		Use:   "settings [team-id]",
		Short: "Update team settings",
		Long: `Update team-wide settings (max concurrent tasks, default branch,
allowed projects). Only members with manage_team permission may change settings.`,
		Args: cobra.ExactArgs(1),
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

			// Start from current settings so unspecified flags are preserved.
			settings := team.Settings
			if cmd.Flags().Changed("max-concurrent-tasks") {
				settings.MaxConcurrentTasks = maxConcurrentTasks
			}
			if cmd.Flags().Changed("default-branch") {
				settings.DefaultBranch = defaultBranch
			}
			if cmd.Flags().Changed("allowed-projects") {
				settings.AllowedProjects = allowedProjects
			}

			if err := service.UpdateTeamSettings(team.ID, actor.ID, settings); err != nil {
				return fmt.Errorf("failed to update settings: %w", err)
			}

			fmt.Printf("✅ Updated settings for team '%s'\n", team.Name)
			fmt.Printf("   Max concurrent tasks: %d\n", settings.MaxConcurrentTasks)
			fmt.Printf("   Default branch:       %s\n", settings.DefaultBranch)
			projects := "all"
			if len(settings.AllowedProjects) > 0 {
				projects = strings.Join(settings.AllowedProjects, ", ")
			}
			fmt.Printf("   Allowed projects:     %s\n", projects)
			return nil
		},
	}

	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have manage_team permission)")
	cmd.Flags().IntVar(&maxConcurrentTasks, "max-concurrent-tasks", 0, "Max concurrent tasks for the team")
	cmd.Flags().StringVar(&defaultBranch, "default-branch", "", "Default branch for the team")
	cmd.Flags().StringSliceVar(&allowedProjects, "allowed-projects", nil, "Allowed project paths (empty = all)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

// newTeamMemberProjectsCmd surfaces Service.UpdateMemberProjects on the CLI.
func newTeamMemberProjectsCmd() *cobra.Command {
	var actorEmail string

	cmd := &cobra.Command{
		Use:   "projects [team-id] [member-email] [project...]",
		Short: "Set a member's project access",
		Long: `Replace the list of projects a member may operate on. Pass no projects
to grant access to all projects. Requires manage_members permission.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]
			memberEmail := args[1]
			projects := args[2:]

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

			member, err := service.GetMemberByEmail(team.ID, memberEmail)
			if err != nil || member == nil {
				return fmt.Errorf("member not found: %s", memberEmail)
			}

			if err := service.UpdateMemberProjects(team.ID, actor.ID, member.ID, projects); err != nil {
				return fmt.Errorf("failed to update member projects: %w", err)
			}

			scope := "all projects"
			if len(projects) > 0 {
				scope = strings.Join(projects, ", ")
			}
			fmt.Printf("✅ %s can now access: %s\n", memberEmail, scope)
			return nil
		},
	}

	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have manage_members permission)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

// newTeamMembershipsCmd surfaces Service.GetTeamsForUser on the CLI.
func newTeamMembershipsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "memberships [email]",
		Short: "List all teams a user belongs to",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			email := args[0]

			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			memberships, err := service.GetTeamsForUser(email)
			if err != nil {
				return fmt.Errorf("failed to get memberships: %w", err)
			}

			if len(memberships) == 0 {
				fmt.Printf("%s is not a member of any team.\n", email)
				return nil
			}

			fmt.Printf("Teams for %s (%d total):\n\n", email, len(memberships))

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "TEAM ID\tTEAM\tROLE\tPROJECTS")
			_, _ = fmt.Fprintln(w, "───────\t────\t────\t────────")
			for _, m := range memberships {
				projects := "all"
				if m.Member != nil && len(m.Member.Projects) > 0 {
					projects = strings.Join(m.Member.Projects, ", ")
				}
				role := teams.Role("")
				if m.Member != nil {
					role = m.Member.Role
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					shortID(m.Team.ID),
					m.Team.Name,
					role,
					projects,
				)
			}
			_ = w.Flush()
			return nil
		},
	}
}

// shortID returns the first 8 characters of an ID, or the whole string if shorter.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
