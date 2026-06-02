package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/teams"
)

func newTeamMemberCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "member",
		Short: "Manage team members",
	}

	cmd.AddCommand(
		newTeamMemberAddCmd(),
		newTeamMemberRemoveCmd(),
		newTeamMemberRoleCmd(),
		newTeamMemberListCmd(),
	)

	return cmd
}

func newTeamMemberAddCmd() *cobra.Command {
	var (
		role       string
		projects   []string
		actorEmail string
	)

	cmd := &cobra.Command{
		Use:   "add [team-id] [email]",
		Short: "Add a member to a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]
			email := args[1]

			if actorEmail == "" {
				return fmt.Errorf("actor email required (use --as)")
			}

			if !teams.Role(role).IsValid() {
				return fmt.Errorf("invalid role: %s (use owner, admin, developer, or viewer)", role)
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

			member, err := service.AddMember(team.ID, actor.ID, email, teams.Role(role), projects)
			if err != nil {
				return fmt.Errorf("failed to add member: %w", err)
			}

			fmt.Printf("✅ Added %s to team '%s' as %s\n", email, team.Name, role)
			fmt.Printf("   Member ID: %s\n", member.ID[:8])

			return nil
		},
	}

	cmd.Flags().StringVar(&role, "role", "developer", "Role: owner, admin, developer, viewer")
	cmd.Flags().StringSliceVar(&projects, "projects", nil, "Restrict to specific projects (empty = all)")
	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have manage_members permission)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

func newTeamMemberRemoveCmd() *cobra.Command {
	var actorEmail string

	cmd := &cobra.Command{
		Use:   "remove [team-id] [member-email]",
		Short: "Remove a member from a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]
			memberEmail := args[1]

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

			if err := service.RemoveMember(team.ID, actor.ID, member.ID); err != nil {
				return fmt.Errorf("failed to remove member: %w", err)
			}

			fmt.Printf("✅ Removed %s from team '%s'\n", memberEmail, team.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have manage_members permission)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

func newTeamMemberRoleCmd() *cobra.Command {
	var actorEmail string

	cmd := &cobra.Command{
		Use:   "role [team-id] [member-email] [new-role]",
		Short: "Change a member's role",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			teamID := args[0]
			memberEmail := args[1]
			newRole := args[2]

			if actorEmail == "" {
				return fmt.Errorf("actor email required (use --as)")
			}

			if !teams.Role(newRole).IsValid() {
				return fmt.Errorf("invalid role: %s (use owner, admin, developer, or viewer)", newRole)
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

			if err := service.UpdateMemberRole(team.ID, actor.ID, member.ID, teams.Role(newRole)); err != nil {
				return fmt.Errorf("failed to update role: %w", err)
			}

			fmt.Printf("✅ Changed %s role from %s to %s\n", memberEmail, member.Role, newRole)
			return nil
		},
	}

	cmd.Flags().StringVar(&actorEmail, "as", "", "Your email (must have manage_members permission)")
	_ = cmd.MarkFlagRequired("as")

	return cmd
}

func newTeamMemberListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [team-id]",
		Short: "List members of a team",
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

			members, err := service.ListMembers(team.ID)
			if err != nil {
				return fmt.Errorf("failed to list members: %w", err)
			}

			if len(members) == 0 {
				fmt.Printf("No members in team '%s'.\n", team.Name)
				fmt.Println()
				fmt.Println("Add a member with:")
				fmt.Printf("   pilot team member add %s <email> --role developer --as you@example.com\n", team.ID[:8])
				return nil
			}

			fmt.Printf("Members of '%s' (%d total):\n\n", team.Name, len(members))

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tEMAIL\tROLE\tPROJECTS")
			_, _ = fmt.Fprintln(w, "──\t─────\t────\t────────")

			for _, m := range members {
				projects := "all"
				if len(m.Projects) > 0 {
					projects = strings.Join(m.Projects, ", ")
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					m.ID[:8],
					m.Email,
					m.Role,
					projects,
				)
			}
			_ = w.Flush()

			return nil
		},
	}
}
