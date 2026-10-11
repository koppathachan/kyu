package queue_test

import (
	"errors"
	"testing"
	"time"

	"github.com/koppathachan/kyu/internal/queue"
)

func TestSettingsRoundTrip(t *testing.T) {
	want := queue.Settings{HoldDuration: 45 * time.Second, AttemptCeiling: 7}
	got, err := queue.DecodeSettings(want.Encode())
	if err != nil {
		t.Fatalf("decode what Encode wrote: %v", err)
	}
	if got != want {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}
}

func TestDefaultsAreRealValues(t *testing.T) {
	if got := queue.DefaultSettings(); got.HoldDuration != queue.DefaultHoldDuration ||
		got.AttemptCeiling != queue.DefaultAttemptCeiling {
		t.Fatalf("DefaultSettings is %+v", got)
	}
	// A zero field is filled in; a set one is left alone.
	got := queue.Settings{AttemptCeiling: 9}.WithDefaults()
	if got.HoldDuration != queue.DefaultHoldDuration || got.AttemptCeiling != 9 {
		t.Fatalf("WithDefaults produced %+v, want the default hold and the ceiling given", got)
	}
}

// A value that will not decode is an error, never a fallback to defaults:
// guessing the ceiling risks delivering past a limit the engine cannot see.
func TestUndecodableSettingsAreNeverGuessedAt(t *testing.T) {
	cases := map[string][]byte{
		"empty":           {},
		"too short":       {queue.SettingsVersion, 0, 0},
		"too long":        make([]byte, queue.SettingsSize+1),
		"a later version": append([]byte{queue.SettingsVersion + 1}, make([]byte, queue.SettingsSize-1)...),
		"a zero ceiling":  queue.Settings{HoldDuration: time.Second}.Encode(),
		"a zero hold":     queue.Settings{AttemptCeiling: 3}.Encode(),
		"a negative hold": queue.Settings{HoldDuration: -time.Second, AttemptCeiling: 3}.Encode(),
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := queue.DecodeSettings(value)
			if err == nil {
				t.Fatalf("decoded %v as %+v, want a refusal", value, got)
			}
			if !errors.Is(err, queue.ErrUndecodableSettings) {
				t.Fatalf("error is %v, want it to be recognisable as undecodable settings", err)
			}
			if got != (queue.Settings{}) {
				t.Fatalf("a refused decode returned %+v, want nothing to fall back on", got)
			}
		})
	}
}
