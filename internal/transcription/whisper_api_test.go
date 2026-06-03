package transcription

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// writeAudioFile materializes a fake audio file under a temp dir and returns its
// path. The contents are arbitrary bytes — the test server never decodes them.
func writeAudioFile(t *testing.T, name string, size int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}
	return path
}

func TestWhisperAPI_Available(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "with key", key: testutil.FakeOpenAIKey, want: true},
		{name: "empty key", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWhisperAPI(tt.key)
			if got := w.Available(); got != tt.want {
				t.Errorf("Available() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWhisperAPI_Name(t *testing.T) {
	if got := NewWhisperAPI(testutil.FakeOpenAIKey).Name(); got != "whisper-api" {
		t.Errorf("Name() = %q, want whisper-api", got)
	}
}

func TestWhisperAPI_Transcribe_NotAvailable(t *testing.T) {
	w := NewWhisperAPI("")
	_, err := w.Transcribe(context.Background(), "/does/not/matter.ogg")
	if err == nil {
		t.Fatal("expected error when API key is empty")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWhisperAPI_Transcribe_FileNotFound(t *testing.T) {
	w := NewWhisperAPI(testutil.FakeOpenAIKey)
	_, err := w.Transcribe(context.Background(), filepath.Join(t.TempDir(), "missing.ogg"))
	if err == nil {
		t.Fatal("expected error for missing audio file")
	}
	if !strings.Contains(err.Error(), "failed to open audio file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWhisperAPI_Transcribe_Success(t *testing.T) {
	audioPath := writeAudioFile(t, "voice.ogg", 64)

	var (
		gotAuth        string
		gotModel       string
		gotRespFormat  string
		gotFileContent bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		// Parse the multipart form to confirm the file + fields are sent.
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("unexpected content type: %q", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read multipart part: %v", err)
			}
			switch part.FormName() {
			case "file":
				b, _ := io.ReadAll(part)
				gotFileContent = len(b) == 64
			case "model":
				b, _ := io.ReadAll(part)
				gotModel = string(b)
			case "response_format":
				b, _ := io.ReadAll(part)
				gotRespFormat = string(b)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello world","language":"en","duration":3.5}`))
	}))
	defer srv.Close()

	w := NewWhisperAPI(testutil.FakeOpenAIKey)
	w.apiURL = srv.URL

	res, err := w.Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}

	if res.Text != "hello world" {
		t.Errorf("Text = %q, want %q", res.Text, "hello world")
	}
	if res.Language != "en" {
		t.Errorf("Language = %q, want en", res.Language)
	}
	if res.Duration != 3.5 {
		t.Errorf("Duration = %v, want 3.5", res.Duration)
	}
	if res.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", res.Confidence)
	}
	if gotAuth != "Bearer "+testutil.FakeOpenAIKey {
		t.Errorf("Authorization = %q, want bearer key", gotAuth)
	}
	if gotModel != "whisper-1" {
		t.Errorf("model field = %q, want whisper-1", gotModel)
	}
	if gotRespFormat != "verbose_json" {
		t.Errorf("response_format field = %q, want verbose_json", gotRespFormat)
	}
	if !gotFileContent {
		t.Error("server did not receive the audio file bytes")
	}
}

func TestWhisperAPI_Transcribe_EstimatesDurationWhenMissing(t *testing.T) {
	// 32000 bytes => exactly 1s at the 32000 bytes/sec estimate.
	audioPath := writeAudioFile(t, "voice.wav", 32000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No duration field => zero => triggers size-based estimation.
		_, _ = w.Write([]byte(`{"text":"x","language":"en"}`))
	}))
	defer srv.Close()

	w := NewWhisperAPI(testutil.FakeOpenAIKey)
	w.apiURL = srv.URL

	res, err := w.Transcribe(context.Background(), audioPath)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if res.Duration != 1.0 {
		t.Errorf("estimated Duration = %v, want 1.0 (32000 bytes / 32000)", res.Duration)
	}
}

func TestWhisperAPI_Transcribe_HTTPError(t *testing.T) {
	audioPath := writeAudioFile(t, "voice.ogg", 16)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	w := NewWhisperAPI(testutil.FakeOpenAIKey)
	w.apiURL = srv.URL

	_, err := w.Transcribe(context.Background(), audioPath)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("error should mention status 401, got: %v", err)
	}
}

func TestWhisperAPI_Transcribe_MalformedJSON(t *testing.T) {
	audioPath := writeAudioFile(t, "voice.ogg", 16)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	w := NewWhisperAPI(testutil.FakeOpenAIKey)
	w.apiURL = srv.URL

	_, err := w.Transcribe(context.Background(), audioPath)
	if err == nil {
		t.Fatal("expected parse error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("unexpected error: %v", err)
	}
}
