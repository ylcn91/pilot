package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ylcn91/pilot/internal/config"
	"github.com/ylcn91/pilot/internal/teams"
)

func newTeamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Manage teams and permissions",
		Long: `Manage teams, members, and role-based access control.

Teams allow multiple users to collaborate on Pilot with different permission levels:
  - owner:     Full access, can delete team
  - admin:     Manage members and projects
  - developer: Execute tasks on assigned projects
  - viewer:    Read-only access`,
	}

	cmd.AddCommand(
		newTeamCreateCmd(),
		newTeamListCmd(),
		newTeamShowCmd(),
		newTeamDeleteCmd(),
		newTeamMemberCmd(),
		newTeamProjectCmd(),
		newTeamAuditCmd(),
	)

	return cmd
}

// Helper functions

func getTeamService() (*teams.Service, func(), error) {
	configPath := cfgFile
	if configPath == "" {
		configPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Open database
	dbPath := filepath.Join(cfg.Memory.Path, "pilot.db")
	if err := os.MkdirAll(cfg.Memory.Path, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %w", err)
	}

	store, err := teams.NewStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("failed to create team store: %w", err)
	}

	service := teams.NewService(store)

	cleanup := func() {
		_ = db.Close()
	}

	return service, cleanup, nil
}

func findTeam(service *teams.Service, idOrName string) (*teams.Team, error) {
	// Try by ID first
	team, err := service.GetTeam(idOrName)
	if err == nil && team != nil {
		return team, nil
	}

	// Try by name
	team, err = service.GetTeamByName(idOrName)
	if err == nil && team != nil {
		return team, nil
	}

	// Try partial ID match
	allTeams, err := service.ListTeams()
	if err != nil {
		return nil, fmt.Errorf("failed to list teams: %w", err)
	}

	for _, t := range allTeams {
		if strings.HasPrefix(t.ID, idOrName) {
			return t, nil
		}
	}

	return nil, fmt.Errorf("team not found: %s", idOrName)
}
