package main

import (
	"fmt"

	"github.com/ylcn91/pilot/internal/autopilot"
)

// resolveAutopilotEnv applies the active autopilot environment using the
// precedence: --env flag > autopilot.default_environment in config > built-in
// default (whatever Config already carries). When either the flag or the
// configured default selects an environment, autopilot is enabled and the
// environment is resolved via SetActiveEnvironment.
//
// Returns the list of available environment names (for error messaging) and an
// error if the requested environment is unknown. When neither a flag nor a
// config default is provided, it is a no-op and returns nil.
func resolveAutopilotEnv(cfg *autopilot.Config, flagEnv string) ([]string, error) {
	name := flagEnv
	if name == "" {
		name = cfg.DefaultEnvironment
	}
	if name == "" {
		return nil, nil
	}

	cfg.Enabled = true
	if err := cfg.SetActiveEnvironment(name); err != nil {
		return availableAutopilotEnvs(cfg), err
	}
	return nil, nil
}

// availableAutopilotEnvs lists the built-in plus user-defined environment names
// for use in error messages.
func availableAutopilotEnvs(cfg *autopilot.Config) []string {
	envs := []string{"dev", "stage", "prod"}
	for name := range cfg.Environments {
		envs = append(envs, name)
	}
	return envs
}

// printAutopilotEnvError writes the standard guidance shown when an unknown
// environment is requested.
func printAutopilotEnvError(w interface{ Write([]byte) (int, error) }, err error, available []string) {
	fmt.Fprintf(w, "Error: %v\n", err)
	fmt.Fprintf(w, "Available environments: %v\n", available)
	fmt.Fprintf(w, "\nTo add a custom environment, add to autopilot.environments in config.yaml:\n")
	fmt.Fprintf(w, "autopilot:\n  environments:\n    my-env:\n      branch: main\n      require_approval: true\n")
}
