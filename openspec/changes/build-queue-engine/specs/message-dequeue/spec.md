## ADDED Requirements

### Requirement: [ENGINE] A message leaves a queue only when it is acknowledged
The engine SHALL call `Delete` for a message when, and only when, a consumer acknowledges it. No other event SHALL cause a message to be deleted.

Every such `Delete` SHALL name the hold it settles, using the attempt count returned by the claim that delivered the message. A `Delete` may be refused in either of two ways, and both mean the same thing to the consumer: the message is no longer its to settle. `FAILED_PRECONDITION` means the hold lapsed and the message passed to another consumer. `NOT_FOUND` means the message is no longer in the queue, which has two causes: the reaper moved it to the dead letter partition after its attempts ran out, or an earlier acknowledgement of the same message already deleted it and this one is a repeat. The engine SHALL NOT claim to tell them apart. There is no read by id in the SPI, so no call it can make would say which happened.

Both refusals are ordinary for the engine and SHALL NOT be reported as faults. Both SHALL be reported to the consumer, which asked for something that did not happen. `Ack` SHALL fail with `ABORTED`. On a stream, the engine SHALL send a settlement rejection naming the message. Telling the consumer `OK` would tell it that it had settled a message another consumer is at that moment still processing, or that it had settled one sitting in a dead letter queue.

The dead letter case is the one recorded under Risks in `design.md`: a consumer slow rather than dead can have its message transferred while it works, and no fence closes that. The engine cannot keep the work; it can refuse to pretend the work landed.

A repeated acknowledgement therefore receives `ABORTED` although its work did land and its message is gone. That is the deliberate direction to be wrong in. This queue is at-least-once, so a consumer told `ABORTED` may repeat work it had already finished — which the contract already permits, and which is what a lost response would have caused anyway. A consumer told `OK` for a message sitting in `.dlq` would instead be told a guarantee that was not kept. `ABORTED` is never a claim that a settlement happened when it did not.

`ABORTED` and not `FAILED_PRECONDITION`: this is a concurrency conflict, and `FAILED_PRECONDITION` is the status a client receives for a queue whose settings this engine cannot read — an unrelated condition calling for an unrelated response.

#### Scenario: A consumer is told when it lost the message
- **WHEN** a consumer acknowledges a message whose hold lapsed and which another consumer now holds
- **THEN** `Ack` fails with `ABORTED`, or on a stream a settlement rejection naming that message is sent

#### Scenario: A consumer is told when its message was dead lettered under it
- **WHEN** a consumer acknowledges a message the reaper moved to the dead letter partition while it was still working
- **THEN** the `Delete` returns `NOT_FOUND`, and the consumer is told `ABORTED` rather than `OK`

#### Scenario: A repeated acknowledgement is refused rather than confirmed
- **WHEN** a consumer acknowledges a message, the acknowledgement succeeds, and the same acknowledgement is sent again
- **THEN** the second `Delete` returns `NOT_FOUND` and the consumer is told `ABORTED`, because nothing in the SPI distinguishes its own earlier settlement from a dead letter transfer

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

An acknowledgement frees the slot it occupied whether its settlement succeeded or was refused. A refused settlement means the message is no longer this stream's — it went to another consumer or to a dead letter partition — so holding the slot against it would leave a consumer whose holds had all lapsed acknowledging everything, collecting a rejection for each, and then sitting at its limit receiving nothing on a stream that is still open.

Every message sent is a message claimed. Without this limit, one slow consumer attached to a deep queue would claim the entire queue and hold it, and every other consumer would see an empty queue while thousands of messages waited.

#### Scenario: A consumer that never acknowledges receives its limit and no more
- **WHEN** a consumer opens with a limit of ten and acknowledges nothing
- **THEN** it receives exactly ten messages, and the remainder stay claimable by others

#### Scenario: An acknowledgement admits one more message
- **WHEN** a consumer at its limit acknowledges one message
- **THEN** exactly one further message is sent

#### Scenario: A rejected settlement frees its slot
- **WHEN** a consumer at its limit acknowledges a message whose hold had lapsed, and the settlement is refused
- **THEN** a settlement rejection is sent and one further message follows

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
