package main

import (
	"testing"
)

func TestTeamCmdWiresNewSubcommands(t *testing.T) {
	root := newTeamCmd()

	wantTop := map[string]bool{"settings": false, "memberships": false}
	for _, c := range root.Commands() {
		if _, ok := wantTop[c.Name()]; ok {
			wantTop[c.Name()] = true
		}
	}
	for name, found := range wantTop {
		if !found {
			t.Errorf("team command missing subcommand %q", name)
		}
	}

	// member projects lives under `team member`.
	for _, c := range root.Commands() {
		if c.Name() != "member" {
			continue
		}
		hasProjects := false
		for _, sub := range c.Commands() {
			if sub.Name() == "projects" {
				hasProjects = true
			}
		}
		if !hasProjects {
			t.Error("team member command missing 'projects' subcommand")
		}
	}
}

func TestTeamSettingsCmdFlags(t *testing.T) {
	cmd := newTeamSettingsCmd()
	for _, flag := range []string{"as", "max-concurrent-tasks", "default-branch", "allowed-projects"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("team settings missing flag --%s", flag)
		}
	}
	// --as is required.
	cmd.SetArgs([]string{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Error("expected error when required --as flag and team-id arg are missing")
	}
}

func TestTeamMemberProjectsCmdArgs(t *testing.T) {
	cmd := newTeamMemberProjectsCmd()
	if cmd.Flags().Lookup("as") == nil {
		t.Error("team member projects missing --as flag")
	}
	// Requires at least team-id + member-email.
	cmd.SetArgs([]string{"team1"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Error("expected error with fewer than 2 positional args")
	}
}

func TestTeamMembershipsCmdArgs(t *testing.T) {
	cmd := newTeamMembershipsCmd()
	cmd.SetArgs([]string{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Error("expected error when email arg missing")
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abcdefghij"); got != "abcdefgh" {
		t.Errorf("shortID long = %q, want abcdefgh", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Errorf("shortID short = %q, want abc", got)
	}
}
