## ADDED Requirements

### Requirement: [BOTH] Nothing available is a result, not an error
A call that finds no message, no item or no work SHALL return success with an empty result. It SHALL NOT return `NOT_FOUND` and SHALL NOT return any other error status.

This applies to `Claim` and `Scan` on the SPI, and to `Dequeue` on the engine.

An idle queue is the ordinary state of most queues most of the time. If emptiness is reported as an error, then the signal that something is actually wrong is buried under it, every client must special-case one error code as success, and any monitoring built on error rates is meaningless.

#### Scenario: An empty queue succeeds
- **WHEN** a consumer dequeues from a queue with nothing available
- **THEN** the call returns success with no message

#### Scenario: An empty claim succeeds
- **WHEN** `Claim` finds no eligible item
- **THEN** the call returns success with no item

### Requirement: [BOTH] Each outcome carries one defined status
The following statuses SHALL be used with these meanings and no others.

| Status | Meaning |
|---|---|
| `OK` | The call did what was asked, including finding nothing |
| `INVALID_ARGUMENT` | The request is malformed or names a reserved name |
| `NOT_FOUND` | The named partition, queue or item does not exist |
| `ALREADY_EXISTS` | A partition or queue of that name is already present |
| `FAILED_PRECONDITION` | The target exists, but not in the state the caller named — a settlement for a hold that has passed on |
| `UNIMPLEMENTED` | The backend does not serve this contract |
| `UNAVAILABLE` | The backend could not be reached or could not serve the call |
| `INTERNAL` | The backend failed in a way it does not attribute to the request |

A status named in the table SHALL NOT be used for an outcome the table does not name it for.

Statuses gRPC itself produces for the transport rather than for an outcome — `CANCELLED`, `DEADLINE_EXCEEDED` and the like — are outside this table. They SHALL NOT be reused for any outcome the table names. A stream ending in `CANCELLED` is ordinary and SHALL NOT be reported as a fault.

#### Scenario: A reserved name is an argument error, not a conflict
- **WHEN** a queue name ending in `.dlq` is supplied by a client
- **THEN** the call fails with `INVALID_ARGUMENT`

#### Scenario: A cancelled stream is not a fault
- **WHEN** a consumer cancels its `StreamingDequeue` stream
- **THEN** the stream ends with `CANCELLED`, its held messages are released, and nothing is reported as a failure

### Requirement: [ENGINE] The engine distinguishes a broken backend from an ordinary outcome
On `UNAVAILABLE` or `INTERNAL` from the SPI, the engine SHALL surface the failure to its caller and SHALL NOT retry the call. On `NOT_FOUND` from `Move` or `Delete`, and on `FAILED_PRECONDITION` from `Delete` or `Release`, for a message that has already been settled, moved or reclaimed by another consumer, the engine SHALL treat the outcome as ordinary and continue. It SHALL NOT retry such a call, because the condition that refused it will not change.

Retrying against a backend that is down converts one failure into a stream of them and delays the operator learning that anything is wrong. A queue that has lost its storage is not degraded; it is not a queue.

#### Scenario: A downed backend is reported once
- **WHEN** the storage server is unreachable and a consumer dequeues
- **THEN** the consumer receives a failure, and the engine does not retry the SPI call

#### Scenario: A lost race is not an error
- **WHEN** a `Move` returns `NOT_FOUND` because another engine already moved the message
- **THEN** the sweep continues without reporting a failure

#### Scenario: A settlement refused by its fence is not a failure
- **WHEN** a `Delete` is refused with `FAILED_PRECONDITION` because the message's hold lapsed and passed to another consumer
- **THEN** the engine continues without reporting a failure and without retrying the call

### Requirement: [ENGINE] A failing backend is named in what the engine reports
Every failure the engine reports that originated in the SPI SHALL identify the operation and the partition involved, and SHALL preserve the status the backend returned.

The storage implementation is written by the deployment. When something breaks, the first question is which side it broke on, and an error that does not say is an error that costs an afternoon.

#### Scenario: An SPI failure is attributable
- **WHEN** the engine reports a failure that came from the SPI
- **THEN** the report names the operation, the partition and the backend's own status
