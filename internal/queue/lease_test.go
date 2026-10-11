package queue_test

import (
	"testing"

	"github.com/koppathachan/kyu/internal/queue"
)

func TestTokenRoundTrip(t *testing.T) {
	want := queue.Token{Partition: "orders", ID: "abc123", Attempts: 4}
	got, err := queue.DecodeToken(want.Encode())
	if err != nil {
		t.Fatalf("decode what Encode wrote: %v", err)
	}
	if got != want {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}
}

func TestAMalformedTokenIsRecognised(t *testing.T) {
	cases := map[string][]byte{
		"empty":                {},
		"header only":          {queue.TokenVersion, 0, 0, 0, 1, 0, 0},
		"a later version":      {queue.TokenVersion + 1, 0, 0, 0, 1, 0, 1, 'p', 'i'},
		"a partition too long": {queue.TokenVersion, 0, 0, 0, 1, 0xff, 0xff, 'p'},
		"not a token at all":   []byte("hello"),
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := queue.DecodeToken(token); err == nil {
				t.Fatalf("decoded %v as %+v, want a refusal", token, got)
			}
		})
	}
}
