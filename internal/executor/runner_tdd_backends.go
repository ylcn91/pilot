package executor

// resolveTDDBackends wires the four TDD role backends (architect, test-author,
// implementer, qa) from the optional opt-in TDD config. Each role that is
// configured gets its own Backend built via NewStageBackend; every other role
// falls back to the run's single backend, so a nil or disabled TDD config
// leaves all four equal to r.backend (zero behavior change vs. today).
func (r *Runner) resolveTDDBackends(config *BackendConfig) error {
	r.architectBackend = r.backend
	r.testAuthorBackend = r.backend
	r.implementerBackend = r.backend
	r.qaBackend = r.backend
	if config == nil || config.TDD == nil || !config.TDD.Enabled {
		return nil
	}
	roles := []struct {
		stage  *StageConfig
		target *Backend
	}{
		{config.TDD.Architect, &r.architectBackend},
		{config.TDD.TestAuthor, &r.testAuthorBackend},
		{config.TDD.Implementer, &r.implementerBackend},
		{config.TDD.QA, &r.qaBackend},
	}
	for _, role := range roles {
		if role.stage == nil {
			continue
		}
		b, err := NewStageBackend(role.stage, *config)
		if err != nil {
			return err
		}
		*role.target = b
	}
	return nil
}
