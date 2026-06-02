package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newTeamCreateCmd() *cobra.Command {
	var ownerEmail string

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if ownerEmail == "" {
				return fmt.Errorf("owner email is required (use --owner)")
			}

			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			team, owner, err := service.CreateTeam(name, ownerEmail)
			if err != nil {
				return fmt.Errorf("failed to create team: %w", err)
			}

			fmt.Println("✅ Team created successfully!")
			fmt.Println()
			fmt.Printf("   Team ID:   %s\n", team.ID)
			fmt.Printf("   Name:      %s\n", team.Name)
			fmt.Printf("   Owner:     %s (%s)\n", ownerEmail, owner.ID)
			fmt.Println()
			fmt.Println("💡 Add members with:")
			fmt.Printf("   pilot team member add %s <email> --role developer\n", team.ID[:8])

			return nil
		},
	}

	cmd.Flags().StringVar(&ownerEmail, "owner", "", "Owner email address (required)")
	_ = cmd.MarkFlagRequired("owner")

	return cmd
}

func newTeamListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all teams",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			teamList, err := service.ListTeams()
			if err != nil {
				return fmt.Errorf("failed to list teams: %w", err)
			}

			if len(teamList) == 0 {
				fmt.Println("No teams found.")
				fmt.Println()
				fmt.Println("Create one with:")
				fmt.Println("   pilot team create \"My Team\" --owner you@example.com")
				return nil
			}

			fmt.Printf("Found %d team(s):\n\n", len(teamList))

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tCREATED")
			_, _ = fmt.Fprintln(w, "──\t────\t───────")

			for _, t := range teamList {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
					t.ID[:8],
					t.Name,
					t.CreatedAt.Format("2006-01-02"),
				)
			}
			_ = w.Flush()

			return nil
		},
	}
}

func newTeamShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [team-id]",
		Short: "Show team details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]

			service, cleanup, err := getTeamService()
			if err != nil {
				return err
			}
			defer cleanup()

			// Try to find by partial ID or name
			team, err := findTeam(service, teamID)
			if err != nil {
				return err
			}

			members, err := service.ListMembers(team.ID)
			if err != nil {
				return fmt.Errorf("failed to get members: %w", err)
			}

			fmt.Printf("📋 Team: %s\n", team.Name)
			fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			fmt.Printf("   ID:         %s\n", team.ID)
			fmt.Printf("   Created:    %s\n", team.CreatedAt.Format("2006-01-02 15:04"))
			fmt.Printf("   Max Tasks:  %d concurrent\n", team.Settings.MaxConcurrentTasks)
			fmt.Println()

			fmt.Printf("👥 Members (%d):\n", len(members))
			if len(members) == 0 {
				fmt.Println("   (no members)")
			} else {
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "   ID\tEMAIL\tROLE\tPROJECTS")
				for _, m := range members {
					projects := "all"
					if len(m.Projects) > 0 {
						projects = strings.Join(m.Projects, ", ")
					}
					_, _ = fmt.Fprintf(w, "   %s\t%s\t%s\t%s\n",
						m.ID[:8],
						m.Email,
						m.Role,
						projects,
					)
				}
				_ = w.Flush()
			}

			// Show project access entries
			accesses, err := service.ListProjectAccess(team.ID)
			if err == nil && len(accesses) > 0 {
				fmt.Println()
				fmt.Printf("📂 Project Access (%d):\n", len(accesses))
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "   PROJECT PATH\tDEFAULT ROLE")
				for _, a := range accesses {
					_, _ = fmt.Fprintf(w, "   %s\t%s\n", a.ProjectPath, a.DefaultRole)
				}
				_ = w.Flush()
			}

			return nil
		},
	}
}

func newTeamDeleteCmd() *cobra.Command {
	var force bool
	var actorEmail string

	cmd := &cobra.Command{
		Use:   "delete [team-id]",
		Short: "Delete a team (owner only)",
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

			if !force {
				fmt.Printf("⚠️  This will permanently delete team '%s' and all associated data.\n", team.Name)
				fmt.Print("   Type 'yes' to confirm: ")
				var confirm string
				_, _ = fmt.Scanln(&confirm)
				if confirm != "yes" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			if err := service.DeleteTeam(team.ID, actor.ID); err != nil {
				return fmt.Errorf("failed to delete team: %w", err)
			}

			fmt.Printf("✅ Team '%s' deleted.\n", team.Name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation")
	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must be team owner)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}
