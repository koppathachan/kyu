# storage-spi Specification

## Purpose
The contract a deployment implements to give kyu durable storage: eight operations over opaque items, a vocabulary with no queue concepts in it, single-call atomicity for `Claim` and `Move`, `Delete` as the only operation that destroys an item, time supplied by the engine, and positions assigned by storage.
## Requirements
### Requirement: [STORAGE] The storage contract is defined once as IDL, and its package is its version
The kyu-to-storage contract SHALL be defined as Protocol Buffers IDL in `kyu.storage.v1.Storage`, and SHALL be served over gRPC. There SHALL be no second definition of it, in any language, that an implementation may bind instead.

The contract version SHALL be the proto package, and SHALL NOT be reported through any field or method. A change that breaks this contract SHALL take a new package.

A backend serving a different package therefore fails its first call with `UNIMPLEMENTED`, rather than serving a contract kyu did not ask for. No handshake is needed and none SHALL be added.

#### Scenario: The contract carries no version field
- **WHEN** the SPI definition is inspected
- **THEN** no method returns and no message carries a contract-version field

#### Scenario: A backend serving a different package cannot be called
- **WHEN** the engine issues a call to a backend serving a package other than `kyu.storage.v1`
- **THEN** the call fails with `UNIMPLEMENTED` and no item is read or written

### Requirement: [STORAGE] The SPI defines exactly eight operations over opaque items
The SPI SHALL define the method set below and no others.

| Method | Purpose |
|---|---|
| `CreatePartition` | Create one partition and record its settings |
| `ListPartitions` | Return every partition with the settings it was created with |
| `Put` | Append one item to a partition and assign its position |
| `Claim` | Take and hold the first eligible item in one indivisible step |
| `Release` | Clear one item's hold, when the caller names the hold it clears |
| `Delete` | Remove one item, when the caller names the hold it settles |
| `Move` | Relocate one item from one partition to another |
| `Scan` | Return items matching fixed filters, without holding them |

An item SHALL carry a partition, an id, a position, an opaque value, a hold instant and an attempt count, and no other field.

A partition SHALL carry a name and an opaque settings value, and no other field. Storage SHALL store the settings value and return it unchanged, and SHALL NOT interpret, validate or act on its contents. Whatever policy the engine keeps there is unreadable to storage, which is what allows per-queue policy to be durable without any field of the SPI naming a queue concept.

#### Scenario: The SPI exposes the defined method set
- **WHEN** the service definition is inspected
- **THEN** it declares exactly these eight methods and no others

#### Scenario: No operation reads an item by id alone
- **WHEN** the method set is inspected for a by-id read
- **THEN** none is present, because the engine performs none

### Requirement: [STORAGE] The SPI names no queue concept
The SPI SHALL NOT name a queue, a message, a lease, a delivery, an acknowledgement or a dead letter in any method, message, field or enum value. It stores items in partitions. Every queue concept is kyu's, and SHALL reach storage as an argument value rather than as vocabulary.

A backend that stores items admits far less interpretation than one told it is implementing a lease. Where the specification is silent, an implementer reasoning about what a lease ought to do will diverge; an implementer stamping a number on a record will not.

#### Scenario: The vocabulary rule holds across the whole definition
- **WHEN** every identifier in the SPI definition is examined
- **THEN** none of them names a queue, message, lease, delivery, acknowledgement or dead letter

#### Scenario: A queue policy reaches storage as a value
- **WHEN** the engine limits claiming to items below the attempt ceiling
- **THEN** the ceiling travels as an argument to `Claim`, and no field of the SPI names it as a maximum delivery count

### Requirement: [STORAGE] `Claim` takes and holds the first eligible item in one indivisible step
`Claim` SHALL take a partition, an instant `now`, a hold instant, and an attempt ceiling. It SHALL select the item of lowest position in that partition whose hold instant is absent or at or before `now` and whose attempt count is below the ceiling; it SHALL set that item's hold instant to the value given and increment its attempt count; and it SHALL return it.

The selection, the hold and the increment SHALL be indivisible. No two concurrent calls SHALL return the same item.

When no item is eligible, the call SHALL succeed and return no item. This is not an error.

#### Scenario: Concurrent claims never return the same item
- **WHEN** many callers claim from one partition at the same time
- **THEN** each item claimed is returned to exactly one caller

#### Scenario: An item at the ceiling is never returned
- **WHEN** an item's attempt count equals the ceiling given
- **THEN** `Claim` does not return it, and returns the next eligible item instead

#### Scenario: An empty result is a success
- **WHEN** no item in the partition is eligible
- **THEN** the call succeeds carrying no item, and no error status is returned

### Requirement: [STORAGE] `Move` relocates an item across partitions, or does nothing
`Move` SHALL take a source partition, a destination partition and an id. It SHALL remove that item from the source and place it in the destination, carrying its value and attempt count unchanged, and SHALL assign it a position in the destination.

Both halves SHALL take effect together or neither SHALL. An implementation SHALL NOT expose a state in which the item is absent from both partitions, and SHALL NOT leave it present in both.

When the item is not in the source, the call SHALL fail with `NOT_FOUND` and SHALL change nothing. When an item of that id is already present in the destination, the call SHALL fail with `ALREADY_EXISTS` and SHALL change nothing.

An id is preserved across a move, so a destination already holding that id would otherwise end up with two items the engine cannot tell apart. Unreachable while messages only ever move one way; reachable the moment anything moves one back.

#### Scenario: A failed move leaves the item where it was
- **WHEN** a move cannot be completed
- **THEN** the item remains in the source partition, unchanged, and is absent from the destination

#### Scenario: The attempt count survives the move
- **WHEN** an item with an attempt count of five is moved
- **THEN** the item in the destination carries an attempt count of five

#### Scenario: A colliding id in the destination is refused
- **WHEN** `Move` names an id already present in the destination partition
- **THEN** the call fails with `ALREADY_EXISTS` and neither partition is modified

#### Scenario: Moving an item that is gone is reported, not guessed
- **WHEN** `Move` names an id absent from the source partition
- **THEN** the call fails with `NOT_FOUND` and no partition is modified

### Requirement: [STORAGE] `Delete` and `Release` name the hold they settle
`Delete` and `Release` SHALL each take an expected attempt count alongside the partition and id. The call SHALL take effect only if the named item's attempt count equals the value given. Otherwise it SHALL fail with `FAILED_PRECONDITION` and SHALL change nothing.

The expected attempt count SHALL be required. There SHALL be no unconditional form of either call.

Without this, a settlement carries no evidence of which hold it belongs to. A consumer that is slow rather than dead — one whose hold lapsed while it was still working — would destroy a message another consumer had since claimed and was processing. The engine cannot prevent this from memory, because the two consumers may be attached to different engine instances, or to one that has restarted since it granted the hold.

The attempt count serves as the fence because `Claim` increments it in the same indivisible step that grants a hold. It rises by exactly one per hold and never repeats, so it names a hold exactly, and storage has nothing new to generate or store.

#### Scenario: A settlement for a hold that has passed on is refused
- **WHEN** an item claimed at attempt count one is claimed again by another caller, and the first caller deletes it naming attempt count one
- **THEN** the call fails with `FAILED_PRECONDITION`, and the item is present and still held by the second caller

#### Scenario: A settlement for the current hold succeeds
- **WHEN** a caller deletes an item naming the attempt count its own claim returned
- **THEN** the item is removed

#### Scenario: A release cannot clear a hold it does not own
- **WHEN** an item's hold has passed to a second caller and the first releases it naming its own attempt count
- **THEN** the call fails with `FAILED_PRECONDITION` and the second caller's hold stands

### Requirement: [STORAGE] `Delete` is the only operation that destroys an item
No operation other than `Delete` SHALL cause an item to cease to exist. `Claim`, `Release`, `Move` and `Scan` SHALL NOT remove an item under any condition, and no implementation SHALL remove an item on its own initiative — not on expiry, not on a size bound, not on an age bound.

Retention is the deployment's business and is invisible to kyu. An implementation that evicts items independently is not implementing this contract.

#### Scenario: A held item that is never claimed again still exists
- **WHEN** an item's hold instant passes and no further call touches it
- **THEN** the item is still present in its partition

#### Scenario: No implicit eviction
- **WHEN** a partition grows beyond any bound the implementation maintains internally
- **THEN** no item is removed, and `Put` fails rather than making room

### Requirement: [STORAGE] Storage never reads a clock
Every operation whose behaviour depends on time SHALL receive that time as an argument. No response SHALL carry a timestamp, and no implementation SHALL consult its own clock to decide any outcome.

Storage compares the number it was given against the number it stored. It does not know that either is a time.

One clock in the system means two components cannot disagree about whether a hold has lapsed. A backend contributing its own clock reintroduces exactly that disagreement, and its consequence is two consumers holding one message.

#### Scenario: Eligibility follows the caller's instant, not the server's
- **WHEN** `Claim` is given an instant far in the past while items hold instants that have genuinely lapsed in real time
- **THEN** no item is returned, because eligibility is decided against the instant given

#### Scenario: No response carries a time
- **WHEN** every response message in the SPI is inspected
- **THEN** none of them carries a timestamp field

### Requirement: [STORAGE] `Put` takes the caller's id, and refuses a partition that does not exist
`Put` SHALL take the item's id from the caller. Storage SHALL NOT generate one.

A `Put` naming an id already present in that partition SHALL fail with `ALREADY_EXISTS` and SHALL NOT modify the item already there. A `Put` naming a partition that does not exist SHALL fail with `NOT_FOUND` and SHALL NOT create it.

Creating partitions implicitly would turn a misspelled queue name into a silently empty queue, which is the failure `CreateQueue` is strict in order to prevent.

#### Scenario: A duplicate id is refused
- **WHEN** `Put` names an id already present in that partition
- **THEN** it fails with `ALREADY_EXISTS` and the existing item is unchanged

#### Scenario: An unknown partition is not created
- **WHEN** `Put` names a partition that does not exist
- **THEN** it fails with `NOT_FOUND` and no partition is created

### Requirement: [STORAGE] Storage assigns positions, and positions never go backwards
`Put` SHALL assign the item a position, and SHALL return it. Within one partition, positions SHALL be totally ordered, and SHALL increase with the order in which `Put` calls complete: an item whose `Put` completed before another's began SHALL have the lower position.

Positions SHALL be opaque to ordering beyond comparison. The engine SHALL NOT interpret their magnitude, spacing or encoding.

This is the requirement most easily violated by accident and most difficult to observe when it is. An implementation assigning positions from a client-side clock, or from any source not serialised with the write, will reorder messages under skew or concurrency and produce a queue that is out of order rarely, unreproducibly, and without error.

#### Scenario: Sequential puts are ordered
- **WHEN** two `Put` calls complete one after the other in one partition
- **THEN** the second item's position is greater than the first's

Detection belongs at `Put`, where the invariant is stated, and SHALL compare a returned position only against positions whose calls had already completed when this one was issued — the requirement constrains non-overlapping calls, so concurrent enqueues may legitimately complete in either order.

A check at `Claim` would be wrong. A claimed position lower than one claimed before it is ordinary and required: a redelivered message keeps its position and is claimed ahead of newer work, so such a check fires on every redelivery.

#### Scenario: The engine detects a backend that violates this
- **WHEN** a `Put` returns a position lower than one returned by a `Put` to the same partition that had already completed before this one was issued
- **THEN** the engine reports the violation, naming the backend, the partition and both positions

#### Scenario: Redelivery is not mistaken for a violation
- **WHEN** a message's hold lapses and it is claimed again at a position lower than messages claimed since
- **THEN** no violation is reported and the queue continues to be served

#### Scenario: Concurrent enqueues are not mistaken for a violation
- **WHEN** two `Put` calls to one partition overlap and complete in the opposite order to their positions
- **THEN** no violation is reported

### Requirement: [STORAGE] `Scan` reads without holding
`Scan` SHALL take a partition, an instant, an attempt floor and a limit, and SHALL return up to `limit` items of lowest position whose hold instant is absent or at or before the instant given and whose attempt count is at or above the floor.

`Scan` SHALL NOT modify any item: it SHALL NOT set a hold instant and SHALL NOT increment an attempt count.

`Scan` exists for the reaper alone. An implementation should expect it to be called repeatedly on a timer, for every partition, and to return nothing almost every time.

#### Scenario: Scanning changes nothing
- **WHEN** a partition is scanned
- **THEN** no item's hold instant or attempt count differs from before the call

#### Scenario: A scanned item remains claimable
- **WHEN** an item is returned by `Scan` and then claimed
- **THEN** the claim succeeds as though the scan had not happened

### Requirement: [ENGINE] The engine reaches storage through the SPI and through nothing else
The engine SHALL obtain every durable fact about a queue through the eight SPI operations. It SHALL NOT hold a connection, handle, transaction or configuration belonging to a storage implementation, SHALL NOT branch on the identity of the backend it is talking to, and SHALL NOT keep durable state anywhere else.

State the engine holds in memory SHALL be reconstructible from the SPI after a restart, with one exception: purely diagnostic state that affects no queue's behaviour MAY be lost. The position high-water mark the engine keeps in order to detect a backend violating the ordering requirement is such state.

Ordering detection is therefore best-effort, and resumes from the first `Put` to each partition after a restart. No SPI operation SHALL be added to make it durable — a diagnostic does not earn a place in the contract every deployment must implement.

A single side channel, however small, converts a portable engine into one that works properly against the backend it was developed against.

#### Scenario: No backend-specific behaviour
- **WHEN** the engine's source is examined for a branch on backend identity, type or capability
- **THEN** none is present

#### Scenario: A diagnostic may be forgotten across a restart
- **WHEN** the engine restarts
- **THEN** ordering detection resumes from the first `Put` to each partition, and no queue's behaviour differs

#### Scenario: Restart loses nothing durable
- **WHEN** the engine is stopped and started while messages are enqueued and held
- **THEN** it resumes from the SPI alone, with every queue and every message as storage holds them

