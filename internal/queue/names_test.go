package queue_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sasidakh/kyu/internal/queue"
)

func TestTheReservedSuffix(t *testing.T) {
	if got := queue.DLQName("orders"); got != "orders"+queue.DLQSuffix {
		t.Fatalf("the companion of orders is %q", got)
	}
	if !queue.IsDLQ(queue.DLQName("orders")) {
		t.Fatal("a companion's name is not recognised as one")
	}
	if queue.IsDLQ("orders") {
		t.Fatal("an ordinary name was taken for a companion")
	}
}

func TestCheckName(t *testing.T) {
	if err := queue.CheckName("orders"); err != nil {
		t.Fatalf("an ordinary name was refused: %v", err)
	}

	cases := map[string]error{
		"orders.dlq": queue.ErrNameReserved,
		"":           queue.ErrNameEmpty,
	}
	for name, want := range cases {
		err := queue.CheckName(name)
		if err == nil {
			t.Fatalf("the name %q was accepted", name)
		}
		if !errors.Is(err, want) {
			t.Fatalf("the name %q was refused with %v, want it to be %v", name, err, want)
		}
	}
}

// The refusal says which name it is about, so a client reading it does not
// have to guess which of several it got wrong.
func TestAReservedNameIsNamedInTheRefusal(t *testing.T) {
	err := queue.CheckName("orders.dlq")
	if got := err.Error(); !strings.Contains(got, "orders.dlq") || !strings.Contains(got, queue.DLQSuffix) {
		t.Fatalf("the refusal %q does not name the queue and the suffix", got)
	}
}
