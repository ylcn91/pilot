package transcription

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// stubTranscriber is a programmable Transcriber for exercising Service routing.
type stubTranscriber struct {
	name      string
	available bool
	result    *Result
	err       error
	calls     int
}

func (s *stubTranscriber) Transcribe(_ context.Context, _ string) (*Result, error) {
	s.calls++
	return s.result, s.err
}
func (s *stubTranscriber) Name() string    { return s.name }
func (s *stubTranscriber) Available() bool { return s.available }

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.Backend != "auto" {
		t.Errorf("Backend = %q, want auto", c.Backend)
	}
}

func TestNewService(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{name: "nil config has no key", config: nil, wantErr: true},
		{name: "missing key", config: &Config{Backend: "auto"}, wantErr: true},
		{
			name:    "with key",
			config:  &Config{Backend: "whisper-api", OpenAIAPIKey: testutil.FakeOpenAIKey},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewService(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.BackendName() != "whisper-api" {
				t.Errorf("BackendName() = %q, want whisper-api", s.BackendName())
			}
			if !s.Available() {
				t.Error("expected Available() true with a key set")
			}
		})
	}
}

func TestService_Transcribe_PrimarySuccess(t *testing.T) {
	primary := &stubTranscriber{name: "primary", available: true, result: &Result{Text: "ok"}}
	s := &Service{config: DefaultConfig(), primary: primary}

	res, err := s.Transcribe(context.Background(), "audio.ogg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "ok" {
		t.Errorf("Text = %q, want ok", res.Text)
	}
	if primary.calls != 1 {
		t.Errorf("primary called %d times, want 1", primary.calls)
	}
}

func TestService_Transcribe_FallbackUsedOnPrimaryError(t *testing.T) {
	primary := &stubTranscriber{name: "primary", err: errors.New("primary down")}
	fallback := &stubTranscriber{name: "fallback", result: &Result{Text: "from-fallback"}}
	s := &Service{config: DefaultConfig(), primary: primary, fallback: fallback}

	res, err := s.Transcribe(context.Background(), "audio.ogg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "from-fallback" {
		t.Errorf("Text = %q, want from-fallback", res.Text)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Errorf("calls primary=%d fallback=%d, want 1/1", primary.calls, fallback.calls)
	}
}

func TestService_Transcribe_BothFail(t *testing.T) {
	primary := &stubTranscriber{name: "primary", err: errors.New("primary down")}
	fallback := &stubTranscriber{name: "fallback", err: errors.New("fallback down")}
	s := &Service{config: DefaultConfig(), primary: primary, fallback: fallback}

	_, err := s.Transcribe(context.Background(), "audio.ogg")
	if err == nil {
		t.Fatal("expected error when both backends fail")
	}
}

func TestService_Transcribe_PrimaryFailNoFallback(t *testing.T) {
	primary := &stubTranscriber{name: "primary", err: errors.New("boom")}
	s := &Service{config: DefaultConfig(), primary: primary}

	_, err := s.Transcribe(context.Background(), "audio.ogg")
	if err == nil {
		t.Fatal("expected error when primary fails and no fallback")
	}
}

func TestService_Available(t *testing.T) {
	tests := []struct {
		name     string
		primary  Transcriber
		fallback Transcriber
		want     bool
	}{
		{name: "none", want: false},
		{name: "primary available", primary: &stubTranscriber{available: true}, want: true},
		{name: "fallback available", fallback: &stubTranscriber{available: true}, want: true},
		{name: "neither available", primary: &stubTranscriber{available: false}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{config: DefaultConfig(), primary: tt.primary, fallback: tt.fallback}
			if got := s.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestService_BackendName_None(t *testing.T) {
	s := &Service{config: DefaultConfig()}
	if got := s.BackendName(); got != "none" {
		t.Errorf("BackendName() = %q, want none", got)
	}
}

func TestCheckSetup(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantKeySet  bool
		wantRec     string
		wantMissing int
	}{
		{name: "nil config", config: nil, wantKeySet: false, wantRec: "none", wantMissing: 1},
		{name: "no key", config: &Config{}, wantKeySet: false, wantRec: "none", wantMissing: 1},
		{
			name:        "with key",
			config:      &Config{OpenAIAPIKey: testutil.FakeOpenAIKey},
			wantKeySet:  true,
			wantRec:     "whisper-api",
			wantMissing: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := CheckSetup(tt.config)
			if st.OpenAIKeySet != tt.wantKeySet {
				t.Errorf("OpenAIKeySet = %v, want %v", st.OpenAIKeySet, tt.wantKeySet)
			}
			if st.RecommendedBackend != tt.wantRec {
				t.Errorf("RecommendedBackend = %q, want %q", st.RecommendedBackend, tt.wantRec)
			}
			if len(st.Missing) != tt.wantMissing {
				t.Errorf("len(Missing) = %d, want %d", len(st.Missing), tt.wantMissing)
			}
		})
	}
}

func TestFormatStatusMessage(t *testing.T) {
	ready := FormatStatusMessage(&SetupStatus{OpenAIKeySet: true})
	if !strings.Contains(ready, "ready") {
		t.Errorf("ready message missing 'ready': %q", ready)
	}

	notReady := FormatStatusMessage(&SetupStatus{OpenAIKeySet: false})
	if !strings.Contains(notReady, "not set") {
		t.Errorf("not-ready message missing 'not set': %q", notReady)
	}
}

func TestGetInstallInstructions(t *testing.T) {
	out := GetInstallInstructions()
	if !strings.Contains(out, "openai_api_key") {
		t.Errorf("install instructions missing openai_api_key: %q", out)
	}
}
