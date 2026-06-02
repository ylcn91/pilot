package codexruntime

import (
	"context"
	"encoding/json"
)

type ApprovalPolicy string
type ApprovalsReviewer string
type SandboxMode string
type ThreadSource string
type ThreadStartSource string

const (
	ApprovalNever ApprovalPolicy = "never"

	ApprovalsReviewerUser ApprovalsReviewer = "user"

	SandboxReadOnly       SandboxMode = "read-only"
	SandboxWorkspaceWrite SandboxMode = "workspace-write"
	SandboxDangerFull     SandboxMode = "danger-full-access"

	ThreadSourceUser ThreadSource = "user"

	ThreadStartSourceStartup ThreadStartSource = "startup"
	ThreadStartSourceClear   ThreadStartSource = "clear"
)

type ClientInfo struct {
	Name    string  `json:"name"`
	Title   *string `json:"title"`
	Version string  `json:"version"`
}

type InitializeCapabilities struct {
	ExperimentalAPI           bool     `json:"experimentalApi"`
	RequestAttestation        bool     `json:"requestAttestation"`
	OptOutNotificationMethods []string `json:"optOutNotificationMethods,omitempty"`
}

type InitializeParams struct {
	ClientInfo   ClientInfo              `json:"clientInfo"`
	Capabilities *InitializeCapabilities `json:"capabilities"`
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type ThreadStartParams struct {
	Cwd                   string            `json:"cwd,omitempty"`
	ApprovalPolicy        ApprovalPolicy    `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     ApprovalsReviewer `json:"approvalsReviewer,omitempty"`
	Sandbox               SandboxMode       `json:"sandbox,omitempty"`
	Ephemeral             *bool             `json:"ephemeral,omitempty"`
	Model                 string            `json:"model,omitempty"`
	ModelProvider         string            `json:"modelProvider,omitempty"`
	ThreadSource          ThreadSource      `json:"threadSource,omitempty"`
	SessionStartSource    ThreadStartSource `json:"sessionStartSource,omitempty"`
	BaseInstructions      string            `json:"baseInstructions,omitempty"`
	DeveloperInstructions string            `json:"developerInstructions,omitempty"`
	Config                map[string]any    `json:"config,omitempty"`
}

type ThreadStartResponse struct {
	Thread            Thread          `json:"thread"`
	Model             string          `json:"model"`
	ModelProvider     string          `json:"modelProvider"`
	ServiceTier       *string         `json:"serviceTier"`
	Cwd               string          `json:"cwd"`
	ApprovalPolicy    json.RawMessage `json:"approvalPolicy"`
	ApprovalsReviewer string          `json:"approvalsReviewer"`
	Sandbox           json.RawMessage `json:"sandbox"`
}

type Thread struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Preview   string `json:"preview"`
	Ephemeral bool   `json:"ephemeral"`
	Status    any    `json:"status"`
	Cwd       string `json:"cwd"`
}

type UserInput struct {
	Type         string `json:"type"`
	Text         string `json:"text,omitempty"`
	TextElements []any  `json:"text_elements,omitempty"`
	URL          string `json:"url,omitempty"`
	Path         string `json:"path,omitempty"`
	Name         string `json:"name,omitempty"`
}

func TextUserInput(text string) UserInput {
	return UserInput{
		Type:         "text",
		Text:         text,
		TextElements: []any{},
	}
}

func (u UserInput) MarshalJSON() ([]byte, error) {
	if u.Type == "text" {
		type textInput struct {
			Type         string `json:"type"`
			Text         string `json:"text"`
			TextElements []any  `json:"text_elements"`
		}
		elements := u.TextElements
		if elements == nil {
			elements = []any{}
		}
		return json.Marshal(textInput{
			Type:         u.Type,
			Text:         u.Text,
			TextElements: elements,
		})
	}

	type alias UserInput
	return json.Marshal(alias(u))
}

type TurnStartParams struct {
	ThreadID       string         `json:"threadId"`
	Input          []UserInput    `json:"input"`
	Cwd            string         `json:"cwd,omitempty"`
	ApprovalPolicy ApprovalPolicy `json:"approvalPolicy,omitempty"`
	Model          string         `json:"model,omitempty"`
	ServiceTier    string         `json:"serviceTier,omitempty"`
	Effort         string         `json:"effort,omitempty"`
	OutputSchema   any            `json:"outputSchema,omitempty"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type Turn struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	StartedAt   *int64 `json:"startedAt"`
	CompletedAt *int64 `json:"completedAt"`
	DurationMS  *int64 `json:"durationMs"`
}

type TurnSteerParams struct {
	ThreadID       string      `json:"threadId"`
	ExpectedTurnID string      `json:"expectedTurnId"`
	Input          []UserInput `json:"input"`
}

type TurnSteerResponse struct{}

type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type TurnInterruptResponse struct{}

func (c *Client) Initialize(ctx context.Context, params InitializeParams) (InitializeResponse, error) {
	return requestAs[InitializeResponse](ctx, c, "initialize", params)
}

func (c *Client) ThreadStart(ctx context.Context, params ThreadStartParams) (ThreadStartResponse, error) {
	return requestAs[ThreadStartResponse](ctx, c, "thread/start", params)
}

func (c *Client) TurnStart(ctx context.Context, params TurnStartParams) (TurnStartResponse, error) {
	return requestAs[TurnStartResponse](ctx, c, "turn/start", params)
}

func (c *Client) TurnSteer(ctx context.Context, params TurnSteerParams) (TurnSteerResponse, error) {
	return requestAs[TurnSteerResponse](ctx, c, "turn/steer", params)
}

func (c *Client) TurnInterrupt(ctx context.Context, params TurnInterruptParams) (TurnInterruptResponse, error) {
	return requestAs[TurnInterruptResponse](ctx, c, "turn/interrupt", params)
}

func requestAs[T any](ctx context.Context, c *Client, method string, params any) (T, error) {
	var out T
	msg, err := c.Request(ctx, method, params)
	if err != nil {
		return out, err
	}
	if len(msg.Result) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(msg.Result, &out); err != nil {
		return out, err
	}
	return out, nil
}
