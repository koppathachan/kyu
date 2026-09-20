package spi_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sasidakh/kyu/internal/spi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The storage implementation is written by the deployment, so when something
// breaks the first question is which side it broke on.
func TestAnSPIErrorNamesWhereItCameFrom(t *testing.T) {
	backendErr := status.Error(codes.Unavailable, "connection refused")
	err := &spi.Error{Backend: "storage:50052", Op: "Claim", Partition: "orders", Err: backendErr}

	for _, want := range []string{"storage:50052", "Claim", "orders", "connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error %q does not name %q", err.Error(), want)
		}
	}
}

// The backend's own status survives being reported, so a client sees what the
// backend said rather than something the engine invented.
func TestAnSPIErrorPreservesTheBackendStatus(t *testing.T) {
	backendErr := status.Error(codes.FailedPrecondition, "attempt count moved on")
	err := &spi.Error{Backend: "storage:1", Op: "Delete", Partition: "orders", Err: backendErr}

	if got := err.Code(); got != codes.FailedPrecondition {
		t.Fatalf("code is %s, want %s", got, codes.FailedPrecondition)
	}
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("status.Code sees %s, want the backend's own", got)
	}
	if !errors.Is(err, backendErr) {
		t.Fatal("the backend's error is not reachable through the wrapper")
	}
}

// ListPartitions names no partition, and the message stays readable.
func TestAnSPIErrorWithoutAPartition(t *testing.T) {
	err := &spi.Error{Backend: "storage:1", Op: "ListPartitions", Err: status.Error(codes.Internal, "boom")}
	if strings.Contains(err.Error(), "on : ") {
		t.Fatalf("the error reads badly with no partition: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "ListPartitions") {
		t.Fatalf("the error %q does not name the operation", err.Error())
	}
}

// Dial refuses a target it cannot make a client for, rather than returning a
// client that fails later for a reason nobody can trace.
func TestDialRefusesAnEmptyTarget(t *testing.T) {
	if _, err := spi.Dial(""); err == nil {
		t.Fatal("an empty storage target was accepted")
	}
}
