# kyu architecture

This is the detailed account of how kyu is built: its components, what each
one holds, how a request moves through them, and what happens when one of them
fails. It describes the system as it is in the code.

Read the [overview in the README](../README.md#architecture) first. It gives
the three tiers and the two contracts on one page, and this document assumes
them.

Two other sources sit beside this one, and each answers a different question:

| Source | Answers |
|---|---|
| [`openspec/specs/`](../openspec/specs/) | What kyu must do — requirements and scenarios, per capability |
| [`design.md`](../openspec/changes/archive/2026-10-10-build-queue-engine/design.md) | Why it was built this way — the decisions and the alternatives rejected |
| this document | How it is built — what runs, where state lives, how calls flow |

`design.md` is the record of one change and is frozen; this document is kept
in step with the code. Where they differ, this one describes what runs. One
known difference is listed under [Where the design record
differs](#where-the-design-record-differs).

## Contents

- [System context](#system-context)
- [Deployment topology](#deployment-topology)
- [Packages and their dependencies](#packages-and-their-dependencies)
- [Inside the engine](#inside-the-engine)
- [The data model](#the-data-model)
- [A message's life](#a-messages-life)
- [Request flows](#request-flows)
- [The reaper](#the-reaper)
- [Time](#time)
- [Concurrency and replicas](#concurrency-and-replicas)
- [Failure behaviour](#failure-behaviour)
- [Errors](#errors)
- [Configuration](#configuration)
- [How it is tested](#how-it-is-tested)
- [Limits of the current architecture](#limits-of-the-current-architecture)
- [Where the design record differs](#where-the-design-record-differs)

## System context

kyu has three tiers and two contracts between them:

```text
  applications ──── kyu.v1.Kyu ────► engine ──── kyu.storage.v1.Storage ────► storage server
                    (queues,          (kyu,        (partitions, items,          (supplied by the
                     messages,         cmd/kyu)     positions, holds,            deployment)
                     leases, acks)                  attempts)
```

| Tier | Who writes it | What it knows |
|---|---|---|
| Applications | Anyone | Queues, messages, lease tokens |
| Engine | kyu | Every queue rule: ordering, holds, attempts, dead letters |
| Storage server | The deployment | Partitions of opaque items, and nothing about queues |

Both contracts are gRPC services defined in
[`api/`](../api/). [`kyu.proto`](../api/kyu/v1/kyu.proto) is what
applications call; [`storage.proto`](../api/kyu/storage/v1/storage.proto) is
what a deployment implements, and its comments are the normative contract.
Neither is a library, so either side can be written in any language.

The division of labour is the point of the design. Queue semantics exist once,
in the engine. Storage supplies durability and two atomic operations, and is
given every queue decision as an argument rather than asked to make it. Two
deployments on different backends therefore cannot disagree about what a queue
does.

## Deployment topology

```text
   clients ──┬──► kyu replica A ──┐
             ├──► kyu replica B ──┼──► one kyu.storage.v1.Storage service
             └──► kyu replica C ──┘     (however the deployment makes it durable)
```

- **The engine holds no durable state.** Everything that must survive — items,
  holds, attempt counts, positions, per-queue settings — lives in storage.
  What an engine holds in memory (see [the registry](#the-registry)) is
  rebuilt from `ListPartitions` on start, so a replica can be restarted or
  replaced at any time.
- **Replicas share nothing but storage.** They do not talk to each other. A
  queue created through replica A becomes known to B by
  [discovery](#discovering-a-queue). A lease token issued by A can be
  acknowledged through B, because the token carries everything settlement
  needs.
- **Every replica sweeps every queue.** This is safe — concurrent sweeps
  resolve through `NOT_FOUND` — and multiplies the reaper's storage load by
  the replica count. See [Concurrency and replicas](#concurrency-and-replicas).
- **It ships as one binary.** `kyu` is a static Go binary with no runtime
  dependencies, published as a container image, Linux packages with a
  systemd unit, a Homebrew cask and plain archives — see
  [Installing it](../README.md#installing-it).
- **There is no default backend.** `kyu` refuses to start without `-storage`
  or `KYU_STORAGE`. [`cmd/kyu-memstore`](../cmd/kyu-memstore/) is a reference
  server that keeps everything in process memory; it is for tests and for
  trying kyu out, not for deployment.

Both binaries serve plaintext gRPC with server reflection enabled, and the
engine dials storage without transport security. See
[Limits](#limits-of-the-current-architecture).

## Packages and their dependencies

```text
  cmd/kyu
     │
     ▼
  internal/engine ──────────────────────────────┬──────────► kyu/v1
     │            │                             │
     │            ▼                             │
     │      internal/reaper ──────┐             │
     │            │               │             │
     ▼            ▼               ▼             │
  internal/queue ◄─┘        internal/spi        │
  (standard library only)        │              │
                                 ▼              │
                           kyu/storage/v1 ◄─────┘
                                 ▲
                                 │
  cmd/kyu-memstore ──► internal/storage/memory
```

Arrows point from importer to imported. `engine` and `reaper` both import
`queue` and `spi`; `spi` and `queue` import neither each other nor anything
else of kyu's except, for `spi`, the generated storage package.

Stated as rules, which the import lists confirm:

| Package | Imports from kyu | Role |
|---|---|---|
| [`internal/queue`](../internal/queue/) | nothing — standard library only, not even gRPC | The vocabulary: settings, lease tokens, name rules, the position check |
| [`internal/spi`](../internal/spi/) | `kyu/storage/v1` | The only code that calls storage; wraps every failure with backend, operation and partition |
| [`internal/reaper`](../internal/reaper/) | `queue`, `spi` | Sweep pacing and the sweep loop; holds no queue state |
| [`internal/engine`](../internal/engine/) | `queue`, `spi`, `reaper`, both generated packages | The `kyu.v1.Kyu` service, its registry, discovery, streams and configuration |
| [`internal/storage/memory`](../internal/storage/memory/) | `kyu/storage/v1` only | The reference storage server; shares no code with the engine |
| [`kyu/v1`](../kyu/v1/), [`kyu/storage/v1`](../kyu/storage/v1/) | — | Generated code, committed; the only importable packages |

Three properties follow from that table:

- **The engine reaches storage through `spi` and through nothing else.** No
  package under `internal/engine` imports a backend, and the reference server
  is reached over a socket even in tests.
- **The rules can be checked without a server.** `queue` performs no I/O, so
  the name rules, the token format, the settings encoding and the ordering
  check are tested as plain functions.
- **The reaper could become its own binary.** It needs a storage client, a
  clock, a queue name and an attempt ceiling, and nothing from the engine.

## Inside the engine

One `engine.Server` serves `kyu.v1.Kyu` over one storage backend. Its parts:

```text
  engine.Server
  ├── storage    *spi.Client           one gRPC connection to the backend
  ├── now        func() time.Time      the only clock in the system
  ├── registry   *registry             what this engine knows about queues
  ├── positions  *queue.PositionWatch  diagnostic check on storage's ordering
  ├── sweeper    *reaper.Sweeper       shared by every queue's sweep goroutine
  └── lifetime   context               cancelled by Close; ends every goroutine
```

### The registry

[`registry.go`](../internal/engine/registry.go) holds four facts that always
change together, under one mutex:

| Map | Holds | Set by | Cleared by |
|---|---|---|---|
| `served` | Settings of every queue this engine can serve | `adopt` | `markUnreadable` |
| `unreadable` | Queues that exist but whose settings will not decode | `markUnreadable` | `adopt` |
| `absent` | Names storage did not have, each with an expiry | `rememberAbsent` | `adopt`, or the expiry passing |
| `sweeps` | The stop function of each running sweep | `adopt` (via `startSweep`) | `markUnreadable`, `Close` |

`lookup(name, now)` returns one of four standings — `served`, `unreadable`,
`absent`, or `unknown` — and every handler that names a queue starts there.
`unknown` never means the queue does not exist; it means this engine has to
ask storage.

Everything in the registry is a cache of storage. Losing it on restart costs
one `ListPartitions`.

### Goroutines

| Goroutine | How many | Started by | Ends when |
|---|---|---|---|
| gRPC handlers | One per in-flight call | grpc-go | The call returns |
| Stream receiver | One per open `StreamingDequeue` | the stream handler | The stream ends |
| Discovery loop | One per engine | `Start` | `Close` |
| Sweep | One per servable, non-`.dlq` queue | `registry.adopt` | `Close`, or the queue becoming unreadable |

`Close` cancels `lifetime` and waits for every discovery and sweep goroutine
before closing the storage connection.

### Locks

- `registry.mu` guards the four maps. It is held briefly, and while it is
  held a sweep may be started or stopped — never a storage call.
- `discoverMu` serialises enumeration, so that the discovery timer and a
  client's lookup do not call `ListPartitions` at the same moment.
- A stream's `held` map is touched only by its own handler goroutine and needs
  no lock.
- `PositionWatch` has its own mutex.

## The data model

### What storage holds

A queue named `orders` is two partitions, `orders` and `orders.dlq`. Storage is
not told they are related. Each partition holds items:

| Item field | Set by | Meaning to the engine |
|---|---|---|
| `id` | the engine, at `Put` | The message id: 16 random bytes, hex. Survives a move |
| `position` | storage, at `Put` and `Move` | Order within one partition. Reassigned on a move |
| `value` | the engine, at `Put` | The payload, never read by storage |
| `hold_until` | `Claim` sets it; `Release` clears it | Unix nanoseconds, the engine's instant plus the hold duration |
| `attempts` | `Claim` increments it | Delivery count, and the fence for `Delete` and `Release` |

A partition also carries `settings`: bytes storage stores and returns without
reading.

### Queue settings

[`settings.go`](../internal/queue/settings.go) encodes a queue's policy into
those bytes:

```text
  byte 0      version (1)
  bytes 1–8   hold duration, nanoseconds, big-endian
  bytes 9–12  attempt ceiling, big-endian
```

Both partitions of a queue get the same value. Defaults are written in at
creation, so the bytes are the whole policy and no engine assumes anything.
A value that is the wrong length, a later version, a non-positive hold or a
zero ceiling does not decode, and the queue is
[not served](#failure-behaviour) rather than served on guessed defaults.

### Lease tokens

[`lease.go`](../internal/queue/lease.go) defines the token a delivery carries:

```text
  byte 0      version (1)
  bytes 1–4   attempts at the time of the claim, big-endian
  bytes 5–6   partition name length, big-endian
  then        partition name, then message id
```

The token is everything `Ack` needs: which partition, which item, and which
hold. Because nothing about the hold is kept in engine memory, a token
survives an engine restart and can be settled through any replica. Tokens are
opaque to consumers and are not signed; one that does not decode is
`INVALID_ARGUMENT`.

### Ids and positions

The engine assigns the **id** and it never changes, so a message is still
recognisable in the dead letter partition. Storage assigns the **position**,
which orders items within a partition and is reassigned when an item moves. A
consumer sees the id; the position is returned by `Enqueue` only.

## A message's life

A message is in one of four states, and only the calls on the arrows move it:

```text
                            Put
                             │
                             ▼
                      ┌──────────────┐
      ┌──────────────►│   eligible   │◄───── hold_until ≤ now
      │               └──────┬───────┘       (not an event — the
      │                      │                next Claim finds it)
      │                      │ Claim: attempts += 1, hold set
      │                      ▼
      │               ┌──────────────┐
      └─── Release ───┤     held     ├──── Delete(expect = attempts) ──► gone
         (attempts    └──────┬───────┘
          unchanged)         │ the claim that reached the ceiling,
                             │ and then the hold lapsing
                             ▼
                      ┌──────────────────┐  Scan + Move  ┌───────────┐
                      │  at the ceiling  ├──────────────►│  in .dlq  │
                      │ Claim refuses it │  (the reaper) │ terminal  │
                      └──────────────────┘               └───────────┘
```

Three things in that picture carry the design:

- **Nothing watches a hold.** A lapsed hold is a number that is now in the
  past. The next `Claim` treats the item as eligible; no timer fires.
- **`Claim` is given the ceiling.** An item at its last attempt is
  undeliverable at the backend, so correctness does not depend on the reaper
  ever running. The reaper only makes the move to `.dlq` timely.
- **`Delete` is reached only from `Ack`.** No other path in the engine
  destroys a message. Dead-lettering moves, a broken stream releases, and an
  unacknowledged message stays put.

## Request flows

### Discovering a queue

Every call that names a queue — `Enqueue`, `Dequeue`, the `Open` of a stream —
resolves it through `settingsFor` in
[`discovery.go`](../internal/engine/discovery.go):

```text
  name ──► registry.lookup(name, now)
             │
             ├─ served ─────► use these settings
             ├─ unreadable ─► FAILED_PRECONDITION
             ├─ absent ─────► NOT_FOUND, no storage call
             └─ unknown
                  │
                  ▼
             discover(): ListPartitions, then for each non-.dlq partition
                  │       decode its settings → adopt, or → markUnreadable
                  ▼
             registry.lookup again
                  ├─ served ─────► use these settings
                  ├─ unreadable ─► FAILED_PRECONDITION
                  └─ otherwise ──► remember absent for KYU_NEGATIVE_CACHE_TTL,
                                   NOT_FOUND
```

The same `discover()` also runs once at `Start` and again every
`KYU_DISCOVERY_INTERVAL`. The timer is what starts a sweep on this replica for
a queue created on another one, even if no client ever names it here.
`adopt` clears any remembered absence, so a queue this engine has just created
is never refused by it.

### CreateQueue

```text
  CreateQueue("orders", hold, ceiling)
     │ CheckName: non-empty, not ending in .dlq      else INVALID_ARGUMENT
     │ settings = given values, defaults filled in, encoded
     ├─► CreatePartition("orders.dlq", settings)     ALREADY_EXISTS tolerated
     ├─► CreatePartition("orders",     settings)     ALREADY_EXISTS returned
     └─► registry.adopt("orders")                    served; sweep started
```

The dead letter partition is created first. The queue's own partition is what
makes a queue exist, so a creation that dies between the two calls leaves no
half-queue behind, and retrying it completes.

### Enqueue

```text
  Enqueue("orders", payload)
     │ CheckName, settingsFor
     │ id = 16 random bytes
     │ mark = positions.Begin("orders")
     ├─► Put("orders", id, payload) ──► position
     │ positions.Complete(mark, position)            backwards? log it, keep serving
     └─► { id, position }
```

The message is durable when `Put` returns, because storage returned it.
[`PositionWatch`](../internal/queue/ordering.go) compares the new position only
against puts that had completed before this one started, so concurrent
enqueues are not mistaken for a violation. It is diagnostic: in memory,
best-effort, and lost on restart.

### Dequeue and Ack

```text
  Dequeue("orders")
     │ CheckName, settingsFor
     │ now = engine clock
     ├─► Claim("orders", now, now + hold, ceiling)
     │      storage: first item by position whose hold is clear or lapsed
     │               and whose attempts are below the ceiling;
     │               set hold_until, attempts += 1 — in one step
     └─► { message: id, payload, attempt, lease_token }   or {} if nothing

  Ack(lease_token)
     │ DecodeToken                                    else INVALID_ARGUMENT
     ├─► Delete(partition, id, expect = token.attempts)
     │      OK                    ──► {}
     │      FAILED_PRECONDITION   ──► ABORTED   hold passed to another consumer
     │      NOT_FOUND             ──► ABORTED   moved to .dlq, or already acked
     └─     anything else         ──► the backend's status, named
```

The two refusals look the same to the consumer, because the SPI has no read by
id that could tell them apart. Neither is retried: the condition that refused
it will not change.

The fence is what protects a slow consumer's peers:

```text
  consumer A      consumer B        engine                    storage
      │               │                │ Claim ───────────────►│ attempts → 1
      │◄──────────────┼── token(1) ────│                       │
      │ (working)     │     the hold lapses — nothing observes it
      │               │ Dequeue ──────►│ Claim ───────────────►│ attempts → 2
      │               │◄── token(2) ───│                       │
      │ Ack(token 1) ─┼───────────────►│ Delete(expect = 1) ──►│ count is 2
      │◄──────────────┼──── ABORTED ───│◄─ FAILED_PRECONDITION │ unchanged
```

### StreamingDequeue

[`streaming.go`](../internal/engine/streaming.go) runs one loop per stream:

```text
  consumer                                  engine (one handler goroutine)
     │ Open(queue, in_flight = N) ─────────►│ settingsFor; limit = max(N, 1)
     │                                      │
     │                                      │ ┌─ fill: while held < limit,
     │◄───────────────────────── message ───│ │  Claim and send
     │◄───────────────────────── message ───│ │
     │                                      │ └─ queue empty? poll in 100ms
     │                                      │    window full? wait for an Ack
     │ Ack(token) ─────────────────────────►│ Delete with the fence; slot freed
     │◄──────────────── settlement_rejection│   either way; rejection sent if
     │                                      │   the Delete was refused
     │                                      │
     ✗ stream ends or breaks                │ Release every held message,
                                            │ on a fresh 5s context
```

Rules the loop keeps:

- **The first message must be `Open`.** Anything else, or a second `Open`,
  ends the stream with `INVALID_ARGUMENT`.
- **The limit is never exceeded.** Every message sent is a message claimed, so
  nothing more is claimed until an acknowledgement frees a slot, however deep
  the queue is.
- **A refused settlement still frees its slot.** The message is no longer this
  stream's, and holding the slot would leave a consumer stuck at its limit on
  an open stream.
- **An acknowledgement for a message this stream does not hold is ignored**,
  and does not end the stream.
- **A broken stream releases at once.** `Release` names the hold it clears, so
  a stream whose holds already lapsed cannot disturb the consumer that has
  since claimed them. `FAILED_PRECONDITION` and `NOT_FOUND` from `Release` are
  ordinary.

`Release` is a fast path. If the engine itself dies, nothing releases, and
the holds lapse on their own.

Unary and streaming share `claim` and `settle`, so they apply identical
semantics. The stream reports a refused settlement as a
`settlement_rejection` message where `Ack` would return `ABORTED`.

## The reaper

[`internal/reaper`](../internal/reaper/) moves messages that have spent their
attempts to `.dlq`. One goroutine sweeps each servable queue:

```text
  wait a random fraction of the floor
  loop:
     Scan(queue, now, attempt_floor = ceiling, limit = KYU_SCAN_LIMIT)
        storage: items whose hold has lapsed and whose attempts ≥ ceiling;
                 changes nothing
     for each item: Move(queue, queue.dlq, id)
        OK              moved, with id, payload and attempts intact
        NOT_FOUND       another replica moved it, or it was acked — ignored
        ALREADY_EXISTS  two messages share an id — logged, not retried
        other           logged
     found anything?  interval = floor
     found nothing?   interval = min(interval × 2, ceiling)
     wait between half the interval and the whole interval
```

- **Pacing.** The floor is `KYU_SWEEP_INTERVAL` (1s) and the ceiling
  `KYU_SWEEP_INTERVAL_MAX` (30s). An idle queue settles at about one scan
  every 15–30 seconds; a queue that finds work returns to the floor. The
  random start and jittered waits keep thousands of sweeps from firing on the
  same tick.
- **Backlog.** Each sweep moves at most `KYU_SCAN_LIMIT` (100) items, and a
  sweep that found anything runs again at the floor, so a backlog clears over
  several sweeps.
- **Which queues.** `.dlq` partitions are never swept. A queue whose settings
  cannot be read has its sweep stopped.
- **No timing is promised.** How soon a message reaches `.dlq` depends on the
  interval, the backlog and the backend's speed.
- **It can be turned off.** `engine.WithSweeping(false)` stops transfers and
  changes nothing about which messages can be delivered.

## Time

The engine's clock is the only one in the system. Every time-dependent SPI
call is given the instant to decide against:

| Call | Time it is given |
|---|---|
| `Claim` | `now`, and `hold_until = now + hold duration` |
| `Scan` | `now` |
| everything else | none |

No SPI response carries a time, and the reference server imports no clock.
Storage compares numbers it was given with numbers it was given earlier, so
the engine and storage can never disagree about whether a hold has lapsed.

That guarantee is per engine. Across replicas, each engine reads its own
clock, and nothing reconciles them. A replica whose clock runs ahead sees
another replica's holds lapse early by the amount of the skew, and can
redeliver a message while its first consumer is still within the hold it was
promised. That is a duplicate delivery, inside the at-least-once guarantee;
the fence still stops the first consumer's late `Ack` from destroying the
second consumer's message. Replicas are expected to run with synchronised
clocks, and skew well below the hold duration.

## Concurrency and replicas

What happens when two actors race, whether they are in one engine or in two
replicas:

| Race | Resolved by | Outcome |
|---|---|---|
| Two consumers claim the head of a queue | `Claim` is atomic in storage | Each gets a different item |
| A slow consumer acks after its hold passed on | The attempt-count fence on `Delete` | `ABORTED` to the slow one; the new holder is unaffected |
| Two replicas sweep the same queue | `Move` is atomic; the loser gets `NOT_FOUND` | Moved once; the loser ignores it |
| An ack and a sweep race for an exhausted message | Whichever reaches storage first | Either deleted, or moved and the ack gets `ABORTED` |
| A broken stream releases after its hold passed on | The fence on `Release` | `FAILED_PRECONDITION`, ignored; the new holder is unaffected |
| A queue is created on A while B remembers it as absent | The short TTL on `absent` | B refuses it for at most `KYU_NEGATIVE_CACHE_TTL` (5s) |
| Two enqueues to one partition overlap | `PositionWatch` compares only against completed puts | No false violation |

One race is not closed and is recorded rather than hidden: a consumer on its
**last** attempt that is slow rather than dead can have its message moved to
`.dlq` once its hold lapses, even though it later finishes the work. Its ack
gets `ABORTED`, and the message sits in `.dlq` with nothing recording that it
succeeded. Closing that needs hold renewal, which kyu does not have.

Delivery is at-least-once throughout. A consumer that finishes work and dies
before acknowledging will see the message again.

## Failure behaviour

| What fails | What kyu does | What is lost |
|---|---|---|
| A consumer crashes mid-stream | Its held messages are released at once | Nothing |
| A unary consumer crashes | The hold lapses after the hold duration | Time only |
| A consumer is slow | Its hold lapses; another consumer may take the message; the late ack gets `ABORTED` | Possibly duplicate work |
| An engine crashes | Holds it granted lapse on their own; tokens it issued settle through any replica | Time only |
| An engine restarts | `Start` re-enumerates; sweeps resume | The diagnostic position history |
| Storage is unreachable at startup | `kyu` logs it and serves anyway; the first call that needs storage retries discovery | Nothing |
| Storage fails a call | The backend's status is returned once, naming the operation and partition; not retried | That call |
| Storage returns a backwards position | Logged with backend, partition and both positions; the queue keeps being served | FIFO order, which kyu cannot restore |
| A queue's settings will not decode | That queue is refused with `FAILED_PRECONDITION` and not swept; every other queue is served | Service of that queue until fixed |
| Two items share an id during a move | Logged with both partitions and the id; not retried | That transfer |

The engine never retries a storage call. A retry would hide a backend fault
from the client and double the load on a backend already failing.

## Errors

Every status a client receives is constructed in one file,
[`internal/engine/errors.go`](../internal/engine/errors.go), which carries the
table from [`specs/error-model`](../openspec/specs/error-model/spec.md) as a
comment. In short:

| Status | Means |
|---|---|
| `OK` | Done as asked — including finding nothing to dequeue |
| `INVALID_ARGUMENT` | Malformed request, reserved name, bad lease token, bad stream opening |
| `NOT_FOUND` | No such queue |
| `ALREADY_EXISTS` | `CreateQueue` for a queue that exists |
| `FAILED_PRECONDITION` | The queue exists but this engine cannot read its settings |
| `ABORTED` | The message is no longer this consumer's to settle |
| `UNAVAILABLE`, `UNIMPLEMENTED`, `INTERNAL` | The backend's own status, passed through with the operation and partition named |

Statuses from storage are interpreted where each call is made —
`dequeue.go` decides what `FAILED_PRECONDITION` from `Delete` means — and
[`spi.Error`](../internal/spi/errors.go) wraps every SPI failure so that the
backend, operation and partition appear in what the client and the logs see.

## Configuration

Each setting is read from the environment first; a flag overrides it.

| Flag | Environment | Default | Governs |
|---|---|---|---|
| `-listen` | `KYU_LISTEN` | `127.0.0.1:50051` | Where `kyu.v1.Kyu` is served |
| `-storage` | `KYU_STORAGE` | none — required | The storage server to dial |
| `-sweep-interval` | `KYU_SWEEP_INTERVAL` | 1s | Sweep floor |
| `-sweep-interval-max` | `KYU_SWEEP_INTERVAL_MAX` | 30s | Sweep ceiling |
| `-scan-limit` | `KYU_SCAN_LIMIT` | 100 | Items one sweep may move |
| `-discovery-interval` | `KYU_DISCOVERY_INTERVAL` | 30s | How often partitions are re-enumerated |
| `-negative-cache-ttl` | `KYU_NEGATIVE_CACHE_TTL` | 5s | How long an absent name is remembered |

Per-queue settings — hold duration (default 30s) and attempt ceiling
(default 5) — are given to `CreateQueue` and stored with the queue. The
in-flight limit (default 1) is given per stream in `Open`. `kyu-memstore`
listens on `-listen` or `KYU_STORAGE_LISTEN`, defaulting to `127.0.0.1:50052`.

## How it is tested

The test architecture mirrors the runtime one:

- **`internal/queue`, `internal/reaper`, `internal/spi`** — unit tests of plain
  functions, with no network.
- **`internal/storage/memory`** — the reference server's handlers, called
  directly, checked against the storage contract.
- **`internal/engine`** — black-box tests through the two things a deployment
  has. The engine is served on a real TCP socket and reached through
  `kyu.v1.Kyu`; it reaches the reference server over a second real socket,
  never in-process. The harness injects the engine's clock so holds lapse
  without sleeping, counts every SPI call and can make storage fail so that
  "not retried" is checkable, restarts engines, and runs two engines on one
  store to stand in for replicas.

Every test file declares `package <name>_test` and uses only the exported API.
Run them with `make test`, which is `go test -race ./...`; CI runs the same
target on every push and pull request.

## Limits of the current architecture

These are properties of what is built, not bugs:

- **One storage backend per engine**, and no production backend ships with
  kyu.
- **No transport security or authentication** on either contract. The engine
  dials storage with insecure credentials, and both servers listen in
  plaintext.
- **Every replica sweeps every queue**, so the reaper's load on storage grows
  with the replica count.
- **The stream polls an empty queue every 100ms.** Nothing notifies the engine
  that a message has arrived.
- **Dead letter partitions grow without bound.** Nothing in `kyu.v1.Kyu` reads,
  redrives or trims them yet.
- **No hold renewal**, which leaves the last-attempt race described under
  [Concurrency and replicas](#concurrency-and-replicas).
- **Out of scope so far:** priorities, delayed delivery, deduplication,
  retention, batching, and judging a third-party backend's conformance.

## Where the design record differs

[`design.md`](../openspec/changes/archive/2026-10-10-build-queue-engine/design.md)
says, under *Risks*, that the engine checks positions returned by `Claim` and
"refuses to continue" when they go backwards. What was built checks the
positions returned by `Put`, and keeps serving the queue after logging the
violation. The reasoning is in [`engine.go`](../internal/engine/engine.go):
taking the queue out of service would turn an ordering fault kyu cannot fix
into an outage kyu caused. The accepted spec,
[`specs/storage-spi`](../openspec/specs/storage-spi/spec.md), agrees with the
code.
