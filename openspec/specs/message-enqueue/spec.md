# message-enqueue Specification

## Purpose
Appending a message to a queue, and the delivery order that follows from the positions storage assigns.
## Requirements
### Requirement: [ENGINE] Enqueue appends a message and reports where it landed
`Enqueue` SHALL take a queue name and an opaque payload, SHALL append it through `Put`, and SHALL return the message id assigned by the engine together with the position assigned by storage.

The engine SHALL assign the message id. The id and the position are distinct: the id names the message for acknowledgement and is stable across a move; the position orders it within one partition and is reassigned when it moves.

#### Scenario: A message is durable before the call returns
- **WHEN** `Enqueue` returns success
- **THEN** the message is present in the partition and would survive an engine restart

#### Scenario: A message keeps its id across a dead letter move
- **WHEN** a message is moved to the dead letter partition
- **THEN** its id is unchanged and its position is one assigned by the destination partition

### Requirement: [ENGINE] Messages are delivered in the order storage assigned them
Within one queue, the engine SHALL always claim the eligible message of lowest position. It SHALL NOT reorder, batch out of order, or prefer any message for any reason. There are no priorities.

Order is therefore exactly as good as the backend's position assignment and no better. The engine adds no ordering of its own and repairs none.

#### Scenario: Two messages are delivered in the order they were enqueued
- **WHEN** two messages are enqueued to an empty queue and then claimed one after the other
- **THEN** the first enqueued is claimed first

#### Scenario: A held message does not stop the one behind it
- **WHEN** the lowest-position message is held by a consumer and another consumer claims
- **THEN** the next eligible message is returned rather than nothing

### Requirement: [ENGINE] A redelivered message keeps its place in line
When a message's hold lapses and it becomes eligible again, its position SHALL NOT change. It SHALL be claimed ahead of every message enqueued after it.

A queue that moved failures to the back would deliver in a different order than it accepted, which is the property the queue exists to provide. The consequence is accepted: a message that keeps failing is claimed ahead of newer work until its attempts are spent, at which point it leaves the queue for the dead letter partition.

#### Scenario: A failed message is retried before newer messages
- **WHEN** the oldest message's hold lapses while newer messages are waiting
- **THEN** the next claim returns the oldest message

#### Scenario: A persistently failing message stops blocking once its attempts are spent
- **WHEN** a message reaches the attempt ceiling
- **THEN** it is no longer claimable, and the next claim returns the message behind it

