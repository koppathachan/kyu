package queue

import (
	"errors"
	"fmt"
	"strings"
)

// DLQSuffix is appended to a queue's name to make its dead letter companion.
//
// It is reserved on every client-supplied name. Without that, "orders.dlq"
// could be both a queue someone created and the place "orders" dead letters
// to, and the two would share one partition.
const DLQSuffix = ".dlq"

// The two ways a queue name can be unusable. They are plain errors rather
// than gRPC statuses: which status a caller should receive is the engine's
// judgement, recorded in one place, and this package does not know whether it
// is being called on behalf of a client at all.
var (
	// ErrNameEmpty is a name with nothing in it.
	ErrNameEmpty = errors.New("queue name is empty")

	// ErrNameReserved is a client-supplied name ending in DLQSuffix.
	ErrNameReserved = errors.New("queue name is reserved for dead letter partitions")
)

// DLQName is the companion partition for a queue.
func DLQName(name string) string { return name + DLQSuffix }

// IsDLQ reports whether a partition name is a dead letter companion. Such a
// partition is the end of the line: it is never swept, because there is
// nowhere for a message to move on from one.
func IsDLQ(partition string) bool { return strings.HasSuffix(partition, DLQSuffix) }

// CheckName validates a queue name a client supplied.
//
// A reserved name is refused whether or not that partition happens to exist:
// the request is malformed, not in conflict with anything.
func CheckName(name string) error {
	if name == "" {
		return ErrNameEmpty
	}
	if IsDLQ(name) {
		return fmt.Errorf("%w: %q ends in %q", ErrNameReserved, name, DLQSuffix)
	}
	return nil
}
