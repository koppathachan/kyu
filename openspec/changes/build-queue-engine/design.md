## Context

kyu's premise is that queue semantics are written once and storage is not
written at all. A deployment supplies durability behind a gRPC interface; the
engine runs its queue logic over it and reaches it through nothing else.

That premise only survives if the interface is small enough that someone
will actually implement it, and precise enough that two implementations of it
cannot produce two different queues. Those two pressures pull in opposite
directions, and most of the decisions below are about where to stand between
them.

One thing shapes everything: **we cannot enforce anything.** A storage server
is someone else's program. If it returns positions out of order, or applies
half of a `Move`, the queue misbehaves and no amount of specification
prevents it. What we can do is make the correct implementation the obvious
one, and put every guarantee that matters inside a single call, where the
shape of the signature states the requirement even to an implementer who did
not read the spec.

## Goals / Non-Goals

**Goals**

- A queue that runs end to end: enqueue, lease, redeliver after failure, dead
  letter on exhaustion, delete on acknowledgement.
- A storage interface a competent engineer can implement in an afternoon, in
  any language, with no query engine and no transaction manager beyond two
  atomic operations.
- One clock in the system.
- The engine reaches storage through the SPI and through nothing else, with
  no side channel, no escape hatch and no backend-specific branch.

**Non-Goals**

- Judging third-party storage implementations. Deferred to its own change.
- Consuming or inspecting dead letter queues. Messages arrive there; what
  happens next is deferred.
- Redrive, priorities, message inspection, retention policy, deduplication,
  delayed delivery, batching.
- Horizontal correctness at scale. Multiple engine replicas are safe but not
  efficient; see the reaper risk below.
- A production storage backend. The in-memory server is a reference.

## Decisions

### The SPI speaks storage, not queues

Every queue concept is carried into the SPI as an argument value rather than
as vocabulary. Storage holds items in partitions; it has no idea one of them
is a queue.

| Queue concept | What storage sees |
|---|---|
| queue | partition |
| message | item |
| enqueue | `Put` |
| lease | `Claim`, and a `hold_until` field |
| delivery count | an `attempts` field |
| max deliveries | an argument to `Claim` |
| acknowledge | `Delete` |
| dead letter | `Move` to another partition |
| the reaper's search | `Scan` |

This is not cosmetic. If the SPI said `Lease` and `DeadLetter`, implementers
would reason about what a lease *ought* to do and diverge in the places the
spec was silent. Storing an item and stamping a number on it admits far less
interpretation. It also keeps the boundary honest: when the north and south
interfaces share vocabulary, the engine looks like a passthrough, and the
question "what is kyu actually doing?" has no visible answer.

*Alternative considered:* the previous iteration's five generic primitives —
`Put`, `Get`, `Update`, `Delete`, `Scan` over items with an attribute map,
preconditions and a `Scan` taking `equals`, `range`, `order_by` and page
tokens. Rejected. That `Scan` is a query language: to be a valid backend you
must implement range bounds with inclusive/exclusive edges, multi-key
ordering and stable pagination. That is not an afternoon's work, and the
whole premise rests on it being one. We keep its vocabulary discipline and
drop its generality.

### Eight operations, and two of them exist for atomicity alone

```
CreatePartition   ListPartitions
Put               Claim      Release
Delete            Move       Scan
```

`Claim` and `Move` could each be built from a read plus a conditional write,
which is what the previous design did. They are single calls because that is
the only enforcement mechanism available to us. An implementer handed
`Move(source, destination, id)` writes a transaction, because one call that
mentions two partitions obviously has to be all-or-nothing. An implementer
handed a read and a write, with a paragraph explaining that they must be
linked, writes two statements and ships.

The same reasoning makes `Claim` atomic rather than scan-then-conditional-
update. Beyond the implementer question, a read-then-claim loop has every
consumer contending on the same head item and retrying, which is worst
exactly when the queue is busiest.

*Alternative considered:* a generic `Claim` taking a predicate and a mutation
as data, so storage stays fully policy-free. Rejected on the same grounds as
the generic `Scan` — a predicate evaluator is real work, and a fixed
signature whose arguments are all supplied by the engine leaks no policy
anyway.

### Settlement is fenced by the attempt count

`Delete` and `Release` both require the caller to state the attempt count it
expects, and storage refuses the call if the item's count differs.

`specs/storage-spi` states the requirement and why it exists. The decision
here is what the fence is made of.

It is the attempt count rather than a token, because `Claim` already
increments it in the same indivisible step that grants the hold. It rises by
exactly one per hold and never repeats, so it names a hold exactly, and
storage has nothing new to generate or store.

It is required rather than optional. An unconditional form is a branch every
implementer must write and a guarantee the engine could forget to ask for, in
exchange for a capability nothing in the engine wants.

*Alternative considered:* an opaque hold token generated by storage on each
`Claim`. Equal in strength, and it obliges every backend to mint and store a
unique value. The attempt count is already there and already unique per hold.

*Not applied to `Move`:* not because the message is safe — it is not. A message
reaches the attempt ceiling under a live hold, so the reaper can move it out
from under a consumer that is slow rather than dead. No fence closes that:
attempts do not change while a message is held, so nothing distinguishes a
lapsed hold from a consumer still working under one. `Move` carries no fence
because none would help, and the exposure is recorded under Risks rather than
argued away. Two sweeps racing each other resolve through `NOT_FOUND`.

### A queue is discovered, not assumed absent

An engine that has never heard of a queue must not conclude it does not exist.
Queues are created on whichever replica a client reached, and every other
replica learns of them only from storage.

So there are two paths, and both are needed. A client naming an unknown queue
triggers a `ListPartitions` before the call is answered, because that client
cannot wait for a timer. Sweeping is covered by re-enumerating periodically,
because a queue this replica never serves would otherwise never be swept here
at all.

*Alternative considered:* declaring discovery to be per-instance-at-startup,
and documenting that a queue is served only by replicas started after it was
created. Coherent, and it makes correct operation depend on restarting every
replica after every `CreateQueue`, which nobody will do.

*Cost:* one `ListPartitions` per interval per replica, and a lookup on a cache
miss. Against the per-queue scan traffic already in the design, both round to
nothing.

A negative cache keeps a client looping on a misspelled name from turning every
call into an enumeration, and it re-introduces in miniature the bug this whole
decision exists to fix: an absence remembered here can be contradicted by a
creation there. So it is bounded and short — five seconds by default — and the
staleness window is stated in the spec rather than left to whoever writes the
cache. A longer TTL would buy nothing; the case it defends against is a tight
retry loop, which five seconds already absorbs.

### A partition's settings are an opaque value

Per-queue policy — hold duration, attempt ceiling — has to be durable, because
the reaper needs it and must survive a restart. It also cannot appear in the
SPI, because no field there may name a queue concept.

Both hold if storage carries a partition's settings as bytes it stores and
returns without reading. The engine encodes its policy into that value and
decodes it on the way back; storage still has no idea one of its partitions is
a queue.

*Alternative considered:* named fields on `CreatePartition` for hold duration
and attempt ceiling. Simpler to read, and it breaks the vocabulary rule
outright — a backend author would be implementing a lease again.

### `Get` is not in the SPI

Nothing in the engine reads an item by id. Acknowledgement deletes;
dead-lettering moves; the reaper scans. An operation the engine does not use
is an operation every implementer must write for nobody.

### The engine owns the clock

`Claim` and `Scan` take `now` as an argument. No SPI response carries a
timestamp, and storage never consults its own clock. Storage compares the
number it was given against the number it stored; it does not know that
either is a time.

*Alternative considered:* the previous design's `server_time_unix_nanos` on
every response. Rejected. It puts a clock in every backend, and two clocks
that disagree by a second disagree about whether a hold has lapsed — which
is the difference between one consumer holding a message and two.

### Expiry is not an event, and the reaper is not what makes the system correct

Nothing watches a hold. An item whose `hold_until` has passed is simply
eligible again the next time `Claim` runs, and `Claim` refuses items whose
`attempts` have reached the ceiling, so no consumer can ever receive a
message past its last attempt.

That is what makes the system correct, and it requires no background process
at all. The reaper exists only for timeliness: without it, a message that
exhausted its attempts sits in the queue, claimable by nobody, until someone
notices. With it, the message is moved to `.dlq` promptly.

**One goroutine per queue**, spawned at `CreateQueue` and, at startup, for
every partition returned by `ListPartitions` — otherwise sweeping silently
stops working after the first restart. `.dlq` partitions get no goroutine;
they are the end of the line and there is nowhere to move anything to.

Each goroutine `Scan`s for items whose hold lapsed with attempts spent, and
`Move`s what it finds. Two engines running the same sweep is safe — the
loser's `Move` returns `NOT_FOUND` and is ignored.

Goroutine count is not the cost; the polling is. Ten thousand queues at a
one-second interval is ten thousand requests per second against a customer's
storage server, almost all returning nothing. So: **jitter** the phase, and
**back off** on empty results — double the interval up to a ceiling, snap
back to the floor on any hit. Busy queues stay responsive; idle ones go
quiet.

**No timing guarantee is published.** How quickly a message reaches `.dlq`
depends on the sweep interval, the backlog, the batch size and the storage
implementation's own speed. Best effort, stated as such.

### `Release` is in, and it is a fast path rather than a guarantee

When a streaming consumer dies, the engine learns immediately — the stream
breaks — but storage still believes that consumer's messages are held, and
will keep believing it until their holds lapse. `Release` lets the engine act
on what it already knows, so a consumer crash costs nothing instead of a full
hold duration.

If the *engine* dies, nobody calls `Release` and the holds lapse on their
own. So the hold is always the real guarantee and `Release` never is. It is
worth its place because consumer crashes are common, engine crashes are not,
and clearing a field is the easiest of the eight operations to implement.

It also gives an explicit negative acknowledgement for free, should that be
wanted later.

### Streaming, and the in-flight limit that makes it safe

`StreamingDequeue` is bidirectional. The consumer's first message is `Open`,
naming the queue and how many unacknowledged messages it will hold; every
message after that is an `Ack`. The engine holds exactly that many out, and
sends one more for each acknowledgement it receives.

The limit is not optional. Every message sent is a message claimed, so
without a cap one slow consumer connecting to a deep queue claims the entire
queue and nobody else gets anything.

Acknowledgements may arrive in any order; each names its message.

*Alternative considered:* unary only. Rejected — but unary is kept alongside,
as `Dequeue` plus `Ack`, because it is far simpler to call from a script or a
language with poor streaming support, and because the two share all their
semantics.

Naming follows Google Pub/Sub's `Pull` / `StreamingPull`, the closest
well-known gRPC precedent for exactly this pair.

### The `.dlq` companion is created by the engine, not inferred by storage

`CreateQueue("orders")` issues two `CreatePartition` calls. Storage is not
told the two are related, because the relationship is a queue concept.
Client-supplied names ending in `.dlq` are refused, so the companion's name
can never collide with a queue someone asked for.

### Per-queue settings live in storage

The reaper needs each queue's attempt ceiling to know what to move, and must
survive a restart, so the settings cannot live in engine memory.
`CreatePartition` records them and `ListPartitions` returns them, so the
reaper learns thresholds from the call it was already making.

### Defaults

| Setting | Default | Where |
|---|---|---|
| hold duration | 30s | per queue, at creation — deliberately not per dequeue |
| attempt ceiling | 5 | per queue, at creation |
| in-flight limit | 1 | per stream, in `Open` |
| sweep floor | 1s | `KYU_SWEEP_INTERVAL`, engine-wide |
| sweep ceiling | 30s | `KYU_SWEEP_INTERVAL_MAX`, engine-wide |
| scan limit | 100 | `KYU_SCAN_LIMIT`, engine-wide |
| discovery interval | 30s | `KYU_DISCOVERY_INTERVAL`, engine-wide |
| negative cache TTL | 5s | `KYU_NEGATIVE_CACHE_TTL`, engine-wide |

Thirty seconds of hold is a compromise: it is the recovery time after an
engine crash, which argues shorter, and the budget a slow consumer gets
before its message is taken away, which argues longer.

## Risks / Trade-offs

**A storage implementation that assigns positions out of order breaks FIFO
silently** → We cannot prevent it. The engine can cheaply detect it: positions
returned by `Claim` within a partition should not go backwards, and the
engine logs loudly and refuses to continue when they do. A baffling
production bug becomes a startup error.

**A naive `Scan` is a full partition scan, run on a timer, usually finding
nothing** → The most likely performance disaster, and entirely in someone
else's code. Mitigated by backoff and jitter, and by saying plainly in the
proto comments what this call does all day and how often.

**Dead letter partitions grow without bound** → By design; nothing deletes
from them and nothing in this change consumes them. Called out here so that
it is a known consequence rather than a discovered one.

**Every engine replica runs every queue's reaper** → Safe, wasteful, and
N times the storage load. Acceptable while replica counts are small. The
reaper touches nothing but the SPI, so lifting it into a single separate
binary later is mechanical.

Note that this is the benign half of replication. The dangerous half was a
queue swept by *no* replica: queues are created on whichever instance a client
reached, so a startup-only enumeration leaves a queue invisible to every
replica started before it, and if the replica that created it goes away, its
messages sit at the ceiling untransferred. Periodic re-enumeration is what
makes the wasteful claim true and the dangerous one impossible; the exposure
is bounded by the discovery interval.

**Two calls per successful message** — `Claim` then `Delete` — plus a third
on failure → The price of not letting storage implement the queue. A fatter
SPI would cut a round trip and would mean each backend reimplementing the
semantics, which is the thing kyu exists to prevent.

**The reaper can move a message out from under a slow consumer** → A message
reaches the ceiling by being claimed, under a hold, by a live consumer. The
reaper moves it once that hold lapses, which is exactly the "slow rather than
dead" case the settlement fence exists for. That consumer's acknowledgement is
then refused, and a message it processed successfully sits in `.dlq` with
nothing recording that it succeeded. Fencing `Move` on the attempt count does
not help — attempts do not move while a message is held — and refusing a
`Move` against a live hold would only narrow the window between `Scan` and
`Move`, not close it. Closing it needs hold renewal, which this change does not
have. Recorded rather than mitigated: it is the same at-least-once seam as
redelivery, and a partial mitigation here would read as a guarantee.

**A message can be delivered twice.** A consumer that finishes work and dies
before acknowledging will have its message redelivered. At-least-once, stated
plainly; exactly-once is not offered and deduplication is out of scope. Fencing
prevents a late settlement from destroying another consumer's message; it does
not prevent the duplicate delivery that produced the second consumer.

## Migration Plan

Nothing is deployed, so there is nothing to migrate. The MongoDB prototype in
`q/` is deleted rather than adapted — it shares no interface with what
replaces it, and its behaviour (mark-read on send, no acknowledgement) is the
thing being fixed.

`pubsub/` is untouched and keeps building throughout.

Order of work: contracts first, then the in-memory storage server, then the
engine, then the reaper. The engine is not written against a mock — it is
written against the reference server over real gRPC from the first test, so
that the SPI is exercised as a wire contract rather than a Go interface.

## Open Questions

1. **Are the defaults right?** Hold duration and attempt ceiling in
   particular are guesses that feel reasonable and have no evidence behind
   them.
2. **Should a queue's settings be versioned inside the opaque value?** They
   are bytes the engine writes and reads back, so a later engine will meet
   values an earlier one wrote. A version byte costs nothing now and cannot be
   added retroactively.

Two questions carried here earlier are closed, and are recorded as decisions
rather than left open:

- *`CreateQueue` idempotency* — decided strict. `specs/queue-provisioning`
  specifies `ALREADY_EXISTS`, on the grounds that strict creation catches a
  misspelled name at the moment it is made rather than silently serving an
  empty queue nobody writes to.
- *An SPI health call* — decided against. `specs/storage-spi` says no
  handshake is needed and none is to be added; a first real call against a
  backend serving another package fails with `UNIMPLEMENTED`, which is the
  fail-fast that a ping would have provided.
- *`Scan`'s limit* — decided. The engine always supplies an explicit limit,
  defaulting to 100 and configurable; storage is never asked to choose a page
  size, so the reaper's throughput does not vary with the backend.
