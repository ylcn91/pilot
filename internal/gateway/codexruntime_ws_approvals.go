package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ylcn91/pilot/internal/codexruntime"
)

func newRuntimeApprovalRegistry() *runtimeApprovalRegistry {
	return &runtimeApprovalRegistry{
		pending: make(map[runtimeApprovalKey]chan runtimeApprovalResponsePayload),
	}
}

func (s *Server) awaitRuntimeApproval(ctx context.Context, session *Session, req codexruntime.Message) (runtimeApprovalResponsePayload, error) {
	requestID, err := runtimeRequestID(req)
	if err != nil {
		return runtimeApprovalResponsePayload{}, err
	}

	ch, cancel, err := s.runtimeApprovals.register(session.ID, requestID)
	if err != nil {
		return runtimeApprovalResponsePayload{}, err
	}
	defer cancel()

	if err := sendRuntimeApprovalRequest(session, req); err != nil {
		return runtimeApprovalResponsePayload{}, err
	}

	timer := time.NewTimer(runtimeApprovalTimeout)
	defer timer.Stop()

	select {
	case response := <-ch:
		return response, nil
	case <-timer.C:
		return defaultRuntimeApprovalResponse(req.Method, requestID), nil
	case <-ctx.Done():
		return runtimeApprovalResponsePayload{}, ctx.Err()
	}
}

func sendRuntimeApprovalRequest(session *Session, req codexruntime.Message) error {
	requestID, _ := runtimeRequestID(req)
	return sendRuntimePayload(session, runtimeProgressPayload{
		Source:    runtimeSource,
		Kind:      "approval_request",
		RequestID: requestID,
		Method:    req.Method,
		Params:    req.Params,
		Choices:   runtimeApprovalChoices(req.Method),
	})
}

func sendRuntimeError(session *Session, err error) error {
	return sendRuntimePayload(session, runtimeProgressPayload{
		Source: runtimeSource,
		Kind:   "error",
		Error:  err.Error(),
	})
}

func sendRuntimePayload(session *Session, payload runtimeProgressPayload) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg, err := json.Marshal(Message{
		Type:    MessageTypeProgress,
		Payload: raw,
	})
	if err != nil {
		return err
	}
	return session.Send(msg)
}

func parseRuntimeSandbox(value string) (codexruntime.SandboxMode, error) {
	switch codexruntime.SandboxMode(value) {
	case codexruntime.SandboxReadOnly:
		return codexruntime.SandboxReadOnly, nil
	case codexruntime.SandboxWorkspaceWrite:
		return codexruntime.SandboxWorkspaceWrite, nil
	case codexruntime.SandboxDangerFull:
		return codexruntime.SandboxDangerFull, nil
	default:
		return "", fmt.Errorf("invalid sandbox %q", value)
	}
}

func (r *runtimeApprovalRegistry) register(sessionID string, requestID int) (<-chan runtimeApprovalResponsePayload, func(), error) {
	key := runtimeApprovalKey{sessionID: sessionID, requestID: requestID}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.pending[key]; ok {
		return nil, nil, fmt.Errorf("runtime approval request %d is already pending", requestID)
	}

	ch := make(chan runtimeApprovalResponsePayload, 1)
	r.pending[key] = ch
	cancel := func() {
		r.mu.Lock()
		delete(r.pending, key)
		r.mu.Unlock()
	}

	return ch, cancel, nil
}

func (r *runtimeApprovalRegistry) resolve(sessionID string, response runtimeApprovalResponsePayload) error {
	key := runtimeApprovalKey{sessionID: sessionID, requestID: response.RequestID}

	r.mu.Lock()
	ch, ok := r.pending[key]
	if ok {
		delete(r.pending, key)
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("runtime approval request %d is not pending", response.RequestID)
	}

	ch <- response
	return nil
}

func parseRuntimeApprovalResponse(task runtimeTaskPayload) (runtimeApprovalResponsePayload, error) {
	if len(task.RequestID) == 0 {
		return runtimeApprovalResponsePayload{}, errors.New("requestId is required")
	}
	requestID, err := codexruntime.ParseID(task.RequestID)
	if err != nil {
		return runtimeApprovalResponsePayload{}, fmt.Errorf("invalid requestId: %w", err)
	}
	if task.Decision == "" && task.Scope == "" {
		return runtimeApprovalResponsePayload{}, errors.New("decision or scope is required")
	}
	return runtimeApprovalResponsePayload{
		RequestID: requestID,
		Decision:  task.Decision,
		Scope:     task.Scope,
	}, nil
}

func runtimeRequestID(req codexruntime.Message) (int, error) {
	if len(req.ID) == 0 {
		return 0, errors.New("request id is required")
	}
	return codexruntime.ParseID(req.ID)
}

func defaultRuntimeApprovalResponse(method string, requestID int) runtimeApprovalResponsePayload {
	switch method {
	case "execCommandApproval", "applyPatchApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Decision: string(codexruntime.ReviewDenied)}
	case "item/commandExecution/requestApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Decision: string(codexruntime.CommandExecutionDecline)}
	case "item/fileChange/requestApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Decision: string(codexruntime.FileChangeDecline)}
	case "item/permissions/requestApproval":
		return runtimeApprovalResponsePayload{RequestID: requestID, Scope: string(codexruntime.PermissionGrantTurn)}
	default:
		return runtimeApprovalResponsePayload{RequestID: requestID}
	}
}

func respondRuntimeServerRequest(client *codexruntime.Client, req codexruntime.Message, response runtimeApprovalResponsePayload) error {
	switch req.Method {
	case "execCommandApproval", "applyPatchApproval":
		return client.RespondReviewApproval(req, codexruntime.ReviewDecision(response.Decision))
	case "item/commandExecution/requestApproval":
		return client.RespondCommandExecutionApproval(req, codexruntime.CommandExecutionApprovalDecision(response.Decision))
	case "item/fileChange/requestApproval":
		return client.RespondFileChangeApproval(req, codexruntime.FileChangeApprovalDecision(response.Decision))
	case "item/permissions/requestApproval":
		scope := response.Scope
		if scope == "" {
			scope = response.Decision
		}
		return client.RespondPermissionsApproval(req, codexruntime.PermissionsApprovalResponse{
			Permissions: codexruntime.GrantedPermissionProfile{},
			Scope:       codexruntime.PermissionGrantScope(scope),
		})
	default:
		return client.RespondError(req, -32601, "unsupported server request", nil)
	}
}

func runtimeApprovalChoices(method string) []string {
	switch method {
	case "execCommandApproval", "applyPatchApproval":
		return []string{"approved", "approved_for_session", "denied", "abort"}
	case "item/commandExecution/requestApproval":
		return []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/fileChange/requestApproval":
		return []string{"accept", "acceptForSession", "decline", "cancel"}
	case "item/permissions/requestApproval":
		return []string{"turn", "session"}
	default:
		return nil
	}
}
