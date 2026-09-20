## ADDED Requirements

### Requirement: [ENGINE] Creating a queue creates its dead letter companion
`CreateQueue` SHALL create two partitions through the SPI: one named for the queue, and one named for the queue with `.dlq` appended. Both SHALL be created before the call returns success.

If the second creation fails, the call SHALL fail, and the engine SHALL NOT report a queue as created when it has nowhere to dead letter to.

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
A queue's hold duration and attempt ceiling SHALL be supplied at creation, recorded by `CreatePartition`, and returned by `ListPartitions` for every partition.

The engine SHALL NOT hold these settings only in memory.

The reaper needs a queue's attempt ceiling to decide what to move, and must keep working across an engine restart. Reading the settings back from the call it already makes to enumerate partitions is the whole of the mechanism.

#### Scenario: Settings survive an engine restart
- **WHEN** the engine is restarted after a queue was created with a non-default attempt ceiling
- **THEN** the reaper applies that ceiling, having read it from `ListPartitions`

#### Scenario: Defaults are recorded, not assumed
- **WHEN** a queue is created without settings
- **THEN** the defaults are written to storage at creation, and `ListPartitions` returns them explicitly
