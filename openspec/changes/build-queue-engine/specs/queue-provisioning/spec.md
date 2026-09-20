## ADDED Requirements

### Requirement: [ENGINE] Creating a queue creates its dead letter companion
`CreateQueue` SHALL create two partitions through the SPI: one named for the queue, and one named for the queue with `.dlq` appended. Both SHALL be created before the call returns success.

If the second creation fails, the call SHALL fail, and the engine SHALL NOT report a queue as created when it has nowhere to dead letter to.

The companion SHALL be created with the same settings value as the queue itself. Nothing in this change consumes a dead letter queue, so no claim is ever made against the companion and its settings go unused; recording them keeps `ListPartitions` uniform and leaves the deferred dead letter design free to decide what they should mean.

Storage SHALL NOT be told that the two partitions are related. The relationship is a queue concept and belongs to the engine.

#### Scenario: Both partitions exist after creation
- **WHEN** a queue named `orders` is created
- **THEN** partitions `orders` and `orders.dlq` both exist

#### Scenario: A half-created queue is not reported as created
- **WHEN** the companion partition cannot be created
- **THEN** `CreateQueue` fails, and no queue named `orders` is reported as available

### Requirement: [ENGINE] The `.dlq` suffix is reserved
`CreateQueue` SHALL reject any client-supplied name ending in `.dlq` with `INVALID_ARGUMENT`. `Enqueue` SHALL reject any queue name ending in `.dlq` with `INVALID_ARGUMENT`.

Reserving the suffix is what keeps a companion partition's name from ever colliding with a partition a client asked for. Without it, `orders.dlq` could be both a queue someone created and the place `orders` dead letters to, and the two would share one partition.

#### Scenario: A reserved name is refused at creation
- **WHEN** a client creates a queue named `orders.dlq`
- **THEN** the call fails with `INVALID_ARGUMENT` and no partition is created

#### Scenario: Dead letter partitions are not enqueued to directly
- **WHEN** a client enqueues to `orders.dlq`
- **THEN** the call fails with `INVALID_ARGUMENT`

### Requirement: [ENGINE] Creating a queue that exists is an error
`CreateQueue` SHALL fail with `ALREADY_EXISTS` when a partition of that name is already present, and SHALL NOT modify its settings.

Strict creation catches a misspelled name at the moment it is made, rather than silently serving an empty queue nobody is writing to.

#### Scenario: A repeated creation is refused
- **WHEN** a queue is created twice with the same name
- **THEN** the second call fails with `ALREADY_EXISTS` and the first queue's settings are unchanged

### Requirement: [BOTH] A queue's settings are durable and are read back from storage
A queue's hold duration and attempt ceiling SHALL be supplied at creation and SHALL be encoded by the engine into the opaque settings value that `CreatePartition` records and `ListPartitions` returns.

The engine SHALL NOT hold these settings only in memory.

Storage SHALL NOT be asked to understand them. The SPI carries a partition's settings as an opaque value precisely so that per-queue policy can be durable without any field of the SPI naming a hold, a delivery or a ceiling — the encoding is the engine's, and storage stores bytes it cannot read.

The reaper needs a queue's attempt ceiling to decide what to move, and must keep working across an engine restart. Decoding the settings returned by the call it already makes to enumerate partitions is the whole of the mechanism.

#### Scenario: Settings survive an engine restart
- **WHEN** the engine is restarted after a queue was created with a non-default attempt ceiling
- **THEN** the reaper applies that ceiling, having read it from `ListPartitions`

#### Scenario: Defaults are recorded, not assumed
- **WHEN** a queue is created without settings
- **THEN** the defaults are encoded into the settings value at creation, and `ListPartitions` returns it

#### Scenario: Storage does not interpret what it stores
- **WHEN** a partition's settings value is inspected in a backend
- **THEN** the backend stores and returns it unchanged, and no field of the SPI names a hold duration or an attempt ceiling

### Requirement: [ENGINE] A queue this engine has not yet seen is looked up, not refused
When a client names a queue this engine holds no settings for, the engine SHALL call `ListPartitions` before answering.

If the queue is present and this engine can decode its settings, the engine SHALL adopt them, start a sweep for it, and serve the call. If it is present and its settings cannot be decoded, the requirement below governs and the queue SHALL NOT be served — discovery finds a queue, it does not exempt one. If it is absent, the engine SHALL fail the call with `NOT_FOUND`.

The engine SHALL NOT infer a queue's absence from its own memory. Queues are created on whichever replica a client happened to reach, so a queue created a moment ago on another replica is real, is durable, and is simply not yet known here.

The engine MAY remember that a name was absent, to keep a client looping on a misspelled queue from issuing one `ListPartitions` per call. Such an entry SHALL expire after a bounded, configurable interval, and that interval SHALL be short.

An absence MAY therefore be served stale for at most that interval: a queue created on another replica just after this one answered `NOT_FOUND` for it stays absent here until the entry expires. That is the same failure this requirement exists to prevent, re-introduced in miniature, which is why the interval is bounded, short and stated rather than left to an implementation.

#### Scenario: A queue created on another replica is served
- **WHEN** a client enqueues to a queue created through a different engine instance since this one started
- **THEN** the engine finds it through `ListPartitions`, adopts its settings, begins sweeping it, and completes the call

#### Scenario: A queue that does not exist is still refused
- **WHEN** a client names a queue absent from `ListPartitions`
- **THEN** the call fails with `NOT_FOUND`

#### Scenario: Discovery does not override an unreadable queue
- **WHEN** discovery finds a queue whose settings this engine cannot decode
- **THEN** the call fails with `FAILED_PRECONDITION`, no sweep is started for it, and it is reported by name

#### Scenario: A remembered absence does not outlive its interval
- **WHEN** a client is told a queue is absent, the queue is then created through another engine instance, and the client retries after the negative-cache interval
- **THEN** the queue is discovered and the call is served

### Requirement: [ENGINE] A queue whose settings the engine cannot read is not served
When `ListPartitions` returns a settings value this engine cannot decode, the engine SHALL NOT serve that queue: it SHALL NOT claim from it, SHALL NOT sweep it, and SHALL fail client calls naming it with `FAILED_PRECONDITION`. It SHALL report the queue by name, and SHALL continue serving every queue it can read.

This is not the same judgement as an ordering violation, which is reported while service continues. There the engine knows what the queue's policy is and can still honour it; here it does not know the attempt ceiling, so claiming would risk delivering past a limit it cannot see. Refusing is forced rather than chosen.

Serving the remaining queues matters: one queue written by a newer engine should not take down every other queue in the deployment.

#### Scenario: An unreadable queue is refused, not guessed at
- **WHEN** `ListPartitions` returns a settings value the engine cannot decode
- **THEN** calls naming that queue fail with `FAILED_PRECONDITION`, no claim or sweep is issued against it, and the queue is named in what the engine reports

#### Scenario: One unreadable queue does not stop the others
- **WHEN** one queue's settings cannot be decoded and others can
- **THEN** every other queue is served and swept normally
