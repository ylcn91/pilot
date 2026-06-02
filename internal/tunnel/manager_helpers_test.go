package tunnel

import (
	"context"
)

// mockProvider implements Provider for testing
type mockProvider struct {
	name        string
	installed   bool
	setupErr    error
	startURL    string
	startErr    error
	stopErr     error
	statusResp  *Status
	statusErr   error
	url         string
	setupCalled bool
	startCalled bool
	stopCalled  bool
}

func (m *mockProvider) Name() string      { return m.name }
func (m *mockProvider) IsInstalled() bool { return m.installed }
func (m *mockProvider) URL() string       { return m.url }

func (m *mockProvider) Setup(ctx context.Context) error {
	m.setupCalled = true
	return m.setupErr
}

func (m *mockProvider) Start(ctx context.Context) (string, error) {
	m.startCalled = true
	if m.startErr != nil {
		return "", m.startErr
	}
	m.url = m.startURL
	return m.startURL, nil
}

func (m *mockProvider) Stop() error {
	m.stopCalled = true
	m.url = ""
	return m.stopErr
}

func (m *mockProvider) Status(ctx context.Context) (*Status, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	if m.statusResp != nil {
		return m.statusResp, nil
	}
	return &Status{Provider: m.name}, nil
}
