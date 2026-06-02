package executor

// OnProgress registers a callback function to receive progress updates during task execution.
// The callback is invoked whenever the execution phase changes or significant events occur.
// Deprecated: Use AddProgressCallback for multi-listener support. This method remains for
// backward compatibility but will overwrite any callback set via OnProgress (not AddProgressCallback).
func (r *Runner) OnProgress(callback ProgressCallback) {
	r.onProgress = callback
}

// AddProgressCallback registers a named callback for progress updates.
// Multiple callbacks can be registered with different names. Use RemoveProgressCallback
// to unregister. This is thread-safe and works alongside the legacy OnProgress callback.
func (r *Runner) AddProgressCallback(name string, callback ProgressCallback) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	if r.progressCallbacks == nil {
		r.progressCallbacks = make(map[string]ProgressCallback)
	}
	r.progressCallbacks[name] = callback
}

// RemoveProgressCallback removes a named callback registered via AddProgressCallback.
func (r *Runner) RemoveProgressCallback(name string) {
	r.progressMu.Lock()
	defer r.progressMu.Unlock()
	delete(r.progressCallbacks, name)
}

// AddTokenCallback registers a named callback for token usage updates.
// Multiple callbacks can be registered with different names. Use RemoveTokenCallback
// to unregister. This is thread-safe.
func (r *Runner) AddTokenCallback(name string, callback TokenCallback) {
	r.tokenMu.Lock()
	defer r.tokenMu.Unlock()
	if r.tokenCallbacks == nil {
		r.tokenCallbacks = make(map[string]TokenCallback)
	}
	r.tokenCallbacks[name] = callback
}

// RemoveTokenCallback removes a named callback registered via AddTokenCallback.
func (r *Runner) RemoveTokenCallback(name string) {
	r.tokenMu.Lock()
	defer r.tokenMu.Unlock()
	delete(r.tokenCallbacks, name)
}

// reportTokens sends token usage updates to all registered callbacks.
func (r *Runner) reportTokens(taskID string, inputTokens, outputTokens int64, modelName string) {
	r.tokenMu.RLock()
	defer r.tokenMu.RUnlock()
	for _, cb := range r.tokenCallbacks {
		cb(taskID, inputTokens, outputTokens, modelName)
	}
}

// SuppressProgressLogs disables slog output for progress updates.
// Use this when a visual progress display is active to prevent log spam.
func (r *Runner) SuppressProgressLogs(suppress bool) {
	r.suppressProgressLogs = suppress
}

// EmitProgress exposes the progress callback for external callers (e.g., Dispatcher).
// This allows the dispatcher to emit progress events using the runner's registered callback.
func (r *Runner) EmitProgress(taskID, phase string, progress int, message string) {
	r.reportProgress(taskID, phase, progress, message)
}
