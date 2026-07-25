package godotconn

import (
	"testing"
	"time"
)

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1600 * time.Millisecond},
		{5, 3200 * time.Millisecond},
		{6, 5 * time.Second}, // capped
		{7, 5 * time.Second}, // stays capped
		{100, 5 * time.Second},
	}

	for _, tt := range tests {
		got := backoffDuration(tt.attempt)
		if got != tt.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Disconnected, "Disconnected"},
		{Connecting, "Connecting"},
		{Connected, "Connected"},
		{Reconnecting, "Reconnecting"},
		{State(99), "State(99)"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestConfiguredMaxReconnectAttempts(t *testing.T) {
	tests := []struct {
		name string
		env  string
		set  bool
		want int
	}{
		{"unset uses bounded default", "", false, defaultMaxReconnectAttempts},
		{"explicit zero means unlimited", "0", true, 0},
		{"positive override", "5", true, 5},
		{"negative falls back to default", "-1", true, defaultMaxReconnectAttempts},
		{"garbage falls back to default", "banana", true, defaultMaxReconnectAttempts},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(maxReconnectAttemptsEnv, tt.env)
			}
			if got := configuredMaxReconnectAttempts(); got != tt.want {
				t.Errorf("configuredMaxReconnectAttempts() = %d, want %d", got, tt.want)
			}
		})
	}
}
