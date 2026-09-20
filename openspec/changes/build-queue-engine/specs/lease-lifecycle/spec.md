## ADDED Requirements

### Requirement: [ENGINE] A message is delivered only under a lease
The engine SHALL NOT deliver a message to a consumer except by claiming it first. A delivered message SHALL be held until its hold instant, and SHALL NOT be delivered to any other consumer while the hold stands.

The hold duration SHALL default to the queue's setting and MAY be overridden per dequeue.

#### Scenario: A held message is invisible to other consumers
- **WHEN** one consumer holds a message and another dequeues from the same queue
- **THEN** the second consumer receives a different message or none

#### Scenario: Nothing is delivered without a claim
- **WHEN** a message reaches a consumer
- **THEN** a `Claim` for it completed before it was sent

### Requirement: [ENGINE] Every claim counts as an attempt
`Claim` increments a message's attempt count as part of the same indivisible step that holds it. The engine SHALL NOT maintain a separate count and SHALL NOT increment on acknowledgement, on failure or on expiry.

Counting at the moment of claiming means the count is exactly the number of times the message was handed out, and it cannot drift from what storage holds — there is no second place for it to be recorded.

#### Scenario: A message claimed three times carries three attempts
- **WHEN** a message has been claimed three times, its hold lapsing between each
- **THEN** its attempt count is three

#### Scenario: The engine holds no counter of its own
- **WHEN** the engine is restarted between two claims of one message
- **THEN** the second claim's attempt count continues from the first, read from storage

### Requirement: [ENGINE] A lapsed hold is not an event
The passing of a hold instant SHALL NOT be a notification, a callback or a state change. No component SHALL be required to observe it.

A message whose hold has lapsed becomes eligible by the definition of `Claim`, and is delivered again the next time somebody claims. Correctness therefore does not depend on any background process running, and a deployment with the reaper disabled still never delivers a message past its attempt ceiling — `Claim` refuses it.

The engine MAY hold in-memory timers for holds it granted, to act at the moment one lapses. Such timers are an optimisation. They SHALL NOT be relied upon, SHALL be discarded on restart, and their absence SHALL change no outcome other than timing.

#### Scenario: No consumers means nothing moves
- **WHEN** a message's hold lapses in a queue with no consumers and no reaper
- **THEN** nothing happens, and the message is delivered when a consumer next dequeues

#### Scenario: Correctness does not depend on the reaper
- **WHEN** the reaper is disabled and a message reaches the attempt ceiling
- **THEN** no consumer ever receives it again

### Requirement: [ENGINE] Release returns a held message immediately
`Release` SHALL clear a message's hold, making it eligible at once. It SHALL NOT change the attempt count and SHALL NOT delete or move the message.

The engine SHALL name the hold it is clearing, using the attempt count returned by the claim that granted it. A release refused with `FAILED_PRECONDITION` means the hold has already lapsed and passed to another consumer; the engine SHALL treat that as an ordinary outcome and SHALL NOT retry it.

The engine SHALL release every message held by a streaming consumer when that consumer's stream ends for any reason, and SHALL release a message when a consumer's hold is explicitly abandoned.

Release is a fast path, not a guarantee. When the engine itself stops, nothing releases anything and holds lapse on their own; the hold instant is what makes the system correct and release only makes it prompt.

#### Scenario: A crashed consumer's messages are available at once
- **WHEN** a streaming consumer holding messages disconnects
- **THEN** those messages are claimable immediately rather than at their hold instants

#### Scenario: Release does not consume an attempt
- **WHEN** a message with one attempt is released and claimed again
- **THEN** its attempt count is two, reflecting the two claims and not the release

#### Scenario: An engine crash falls back to the hold
- **WHEN** the engine stops while messages are held
- **THEN** those messages become claimable when their hold instants pass

#### Scenario: A late release cannot disturb a live hold
- **WHEN** a stream breaks after its messages' holds have already lapsed and another consumer has claimed them
- **THEN** the releases are refused with `FAILED_PRECONDITION`, and the second consumer's holds stand
