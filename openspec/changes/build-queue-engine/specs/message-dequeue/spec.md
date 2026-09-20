## ADDED Requirements

### Requirement: [ENGINE] A message leaves a queue only when it is acknowledged
The engine SHALL call `Delete` for a message when, and only when, a consumer acknowledges it. No other event SHALL cause a message to be deleted.

Every such `Delete` SHALL name the hold it settles, using the attempt count returned by the claim that delivered the message. A `Delete` refused with `FAILED_PRECONDITION` means the message's hold lapsed and it has passed to another consumer; it is no longer this consumer's to settle, and the engine SHALL treat the refusal as an ordinary outcome.

#### Scenario: An unacknowledged message is not deleted
- **WHEN** a consumer receives a message and never acknowledges it
- **THEN** the message still exists, and is delivered again after its hold lapses

#### Scenario: Acknowledgement deletes exactly one message
- **WHEN** a consumer acknowledges a message
- **THEN** that message is deleted and no other message is affected

### Requirement: [ENGINE] Dequeue is offered in a unary and a streaming form
The engine SHALL offer `Dequeue`, returning at most one message with a lease token, acknowledged by a separate `Ack` call; and `StreamingDequeue`, a bidirectional stream.

The lease token SHALL carry everything `Ack` needs to name the hold it settles, and SHALL be opaque to the consumer. Because the token carries the fence rather than the engine's memory, an acknowledgement remains safe across an engine restart and across engine instances.

Both forms SHALL observe identical semantics. Every requirement about claiming, holding, attempt counting, ordering and acknowledgement SHALL hold identically over each, and no requirement SHALL be satisfied by one form and not the other.

When no message is available, `Dequeue` SHALL return success carrying no message.

#### Scenario: The two forms agree
- **WHEN** the same sequence of operations is performed through the unary form and again through the streaming form
- **THEN** the observable outcomes are the same

#### Scenario: An empty queue is not an error
- **WHEN** `Dequeue` is called against an empty queue
- **THEN** it succeeds carrying no message

### Requirement: [ENGINE] A stream opens with a queue and an in-flight limit
The first message a consumer sends on `StreamingDequeue` SHALL be an `Open` naming the queue and the number of unacknowledged messages it will accept at once. Every subsequent message from the consumer SHALL be an acknowledgement.

A stream whose first message is not `Open`, or that sends a second `Open`, SHALL be terminated with `INVALID_ARGUMENT`.

The in-flight limit SHALL be at least one and SHALL default to one.

#### Scenario: A stream that does not open properly is rejected
- **WHEN** a consumer's first message on the stream is an acknowledgement
- **THEN** the stream is terminated with `INVALID_ARGUMENT`

### Requirement: [ENGINE] The engine never exceeds a consumer's in-flight limit
The engine SHALL hold at most the agreed number of unacknowledged messages out to one stream at a time. It SHALL send a further message only when an acknowledgement reduces the number outstanding below the limit.

Every message sent is a message claimed. Without this limit, one slow consumer attached to a deep queue would claim the entire queue and hold it, and every other consumer would see an empty queue while thousands of messages waited.

#### Scenario: A consumer that never acknowledges receives its limit and no more
- **WHEN** a consumer opens with a limit of ten and acknowledges nothing
- **THEN** it receives exactly ten messages, and the remainder stay claimable by others

#### Scenario: An acknowledgement admits one more message
- **WHEN** a consumer at its limit acknowledges one message
- **THEN** exactly one further message is sent

### Requirement: [ENGINE] Acknowledgements may arrive in any order
Each acknowledgement SHALL name the message it settles. The engine SHALL NOT require acknowledgements in the order messages were sent, and SHALL NOT treat an acknowledgement of a later message as settling an earlier one.

An acknowledgement naming a message the consumer does not hold SHALL be ignored, and SHALL NOT terminate the stream.

#### Scenario: Out-of-order acknowledgement settles the right message
- **WHEN** a consumer holding three messages acknowledges the third
- **THEN** the third is deleted and the other two remain held

#### Scenario: A stale acknowledgement cannot destroy another consumer's work
- **WHEN** a consumer acknowledges a message whose hold already lapsed and which another consumer now holds
- **THEN** storage refuses the delete, the message stays with the consumer now holding it, and the stream continues

#### Scenario: A stale acknowledgement is refused after an engine restart
- **WHEN** a consumer acknowledges with a lease token issued before the engine restarted, and the message has since been claimed by another consumer
- **THEN** the delete is refused and the message is not removed

### Requirement: [ENGINE] A broken stream releases what it held
When a `StreamingDequeue` stream ends — cleanly, by error, or by the consumer vanishing — the engine SHALL release every message that stream held and had not acknowledged.

Released messages keep the attempts they have already consumed. A consumer that died on its third attempt at a message leaves it with three.

#### Scenario: Messages held by a lost consumer return at once
- **WHEN** a consumer's stream breaks while it holds four unacknowledged messages
- **THEN** all four become claimable immediately

#### Scenario: Work already done is not lost
- **WHEN** a consumer acknowledges two of four messages and then disconnects
- **THEN** the two acknowledged are deleted and the two unacknowledged are released
