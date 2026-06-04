package codexruntime

import "testing"

// TestMapNotificationErrorFallback exercises the firstRawString fallback that
// populates Event.Error from "message", then "error", then "reason" (in
// priority order), skipping missing or empty keys.
func TestMapNotificationErrorFallback(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "message wins over error and reason",
			raw:  `{"method":"error","params":{"message":"m","error":"e","reason":"r"}}`,
			want: "m",
		},
		{
			name: "error used when message missing",
			raw:  `{"method":"error","params":{"error":"e","reason":"r"}}`,
			want: "e",
		},
		{
			name: "reason used when message and error missing",
			raw:  `{"method":"error","params":{"reason":"r"}}`,
			want: "r",
		},
		{
			name: "empty message falls through to error",
			raw:  `{"method":"error","params":{"message":"","error":"e"}}`,
			want: "e",
		},
		{
			name: "empty error falls through to reason",
			raw:  `{"method":"error","params":{"message":"","error":"","reason":"r"}}`,
			want: "r",
		},
		{
			name: "no error keys yields empty",
			raw:  `{"method":"error","params":{"threadId":"t1"}}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := MapNotification(notification(t, tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if event.Error != tt.want {
				t.Fatalf("Error = %q, want %q", event.Error, tt.want)
			}
		})
	}
}
