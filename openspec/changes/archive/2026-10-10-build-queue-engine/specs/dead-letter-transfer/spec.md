## ADDED Requirements

### Requirement: [ENGINE] A message that exhausts its attempts is moved, never deleted
When a message's attempt count reaches the queue's ceiling and its hold has lapsed, the engine SHALL move it to that queue's `.dlq` partition with `Move`. It SHALL NOT delete it, and SHALL NOT deliver it again.

The moved message SHALL keep its id, its payload and its attempt count.

Preserving the count is deliberate: it is the record of how hard the queue tried, and it is the only evidence of that left once the message has left the queue.

#### Scenario: An exhausted message reaches the dead letter partition intact
- **WHEN** a message with an attempt ceiling of five is claimed five times without acknowledgement
- **THEN** it is present in the `.dlq` partition with the same id, the same payload and an attempt count of five

#### Scenario: Nothing is destroyed on the way
- **WHEN** a message is dead lettered
- **THEN** no `Delete` is issued for it

### Requirement: [ENGINE] A message at the ceiling is never delivered again
`Claim` SHALL be called with the queue's attempt ceiling, so that a message at or above it is not returned. The engine SHALL NOT deliver a message whose attempt count has reached the ceiling under any circumstance, including while it is waiting to be moved.

This is what allows the reaper to be an optimisation rather than a load-bearing component: a message awaiting transfer is already undeliverable, so a delayed transfer delays only its arrival in the dead letter partition, never its correctness.

It does not remove every race. A message reaches the ceiling by being claimed, and that claim handed it to a consumer under a hold; the reaper moves it once that hold lapses. A consumer that is slow rather than dead can therefore have its message moved while it is still working, and its acknowledgement will then be refused. See the risk recorded in `design.md`; it is the same at-least-once seam that redelivery opens, and it is not closed here.

#### Scenario: A consumer cannot claim a message awaiting transfer
- **WHEN** a message has reached the attempt ceiling and has not yet been moved
- **THEN** no consumer receives it

#### Scenario: A delayed transfer changes only timing
- **WHEN** the reaper is stopped for an hour
- **THEN** no message is delivered past its ceiling, and transfers resume when it restarts

### Requirement: [ENGINE] The engine sweeps for exhausted messages without being asked
The engine SHALL run one sweep per queue, each periodically calling `Scan` for messages whose hold has lapsed with attempts at or above that queue's ceiling, and `Move`ing each result to the queue's `.dlq` partition.

A sweep SHALL be started for a queue when it is created, for every queue returned by `ListPartitions` when the engine starts, and for every queue not already swept that a later `ListPartitions` returns. The engine SHALL re-enumerate partitions periodically, at a configurable interval.

Startup enumeration alone is not enough, and its absence fails in the direction that matters. Queues are created on whichever replica a client reached, so a queue created on one replica is unknown to every other replica started before it. Those replicas run no sweep for it; if the replica that created it goes away, the queue is swept by nobody, and messages sit at the attempt ceiling — undeliverable and untransferred — for as long as that lasts. Periodic re-enumeration is one call per interval per replica, against a per-queue scan cost that already dwarfs it.

No sweep SHALL be started for a `.dlq` partition. There is nowhere for a message to move on from one.

#### Scenario: Transfer happens with no consumer present
- **WHEN** a queue with no consumers holds a message that reached its ceiling
- **THEN** the message is moved to the dead letter partition

#### Scenario: Sweeps resume after a restart
- **WHEN** the engine restarts with queues created before it started
- **THEN** each of those queues is swept

#### Scenario: Dead letter partitions are not swept
- **WHEN** the sweeps running are enumerated
- **THEN** none names a partition ending in `.dlq`

### Requirement: [ENGINE] Sweeping is jittered and backs off
Each sweep SHALL begin at a randomised offset within its interval. On a scan returning nothing, a sweep SHALL increase its interval up to a configured ceiling; on a scan returning anything, it SHALL return to the configured floor.

The floor and ceiling SHALL be configurable per deployment.

A deployment with ten thousand queues sweeping every second issues ten thousand requests per second against someone else's storage server, almost all of which find nothing. Jitter spreads that load off a single tick; backoff removes most of it. Both matter because the cost falls on a component we did not write and cannot optimise.

#### Scenario: An idle queue stops being polled frequently
- **WHEN** a queue has returned nothing for many consecutive sweeps
- **THEN** its sweep interval has grown toward the configured ceiling

#### Scenario: A queue that finds work becomes responsive again
- **WHEN** a backed-off sweep finds a message to move
- **THEN** its interval returns to the configured floor

#### Scenario: Sweeps do not align
- **WHEN** many queues exist
- **THEN** their sweeps are distributed across the interval rather than firing together

### Requirement: [ENGINE] Concurrent sweeps are safe and no transfer time is promised
Two engines sweeping one queue SHALL NOT corrupt it. A `Move` for a message another engine has already moved fails with `NOT_FOUND`, and the engine SHALL treat that as an ordinary outcome and continue.

No guarantee SHALL be published about how quickly a message reaches its dead letter partition. The interval, the backlog, the scan limit and the storage implementation's own speed all bear on it, and three of those four are outside kyu's control.

#### Scenario: A losing engine is unharmed
- **WHEN** two engines sweep the same queue and both attempt to move one message
- **THEN** one succeeds, the other receives `NOT_FOUND` and continues, and the message is present exactly once

#### Scenario: A backlog is cleared over several sweeps
- **WHEN** more messages are eligible for transfer than one scan returns
- **THEN** successive sweeps move the remainder, and no message is skipped
