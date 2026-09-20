## Why

kyu is a message queue that runs on storage it does not implement. The queue
semantics — ordering, leasing, attempt counting, dead-lettering — are written
exactly once, in an engine we ship. Durability is written by whoever deploys
it, behind a gRPC interface they implement themselves or take from a third
party. Because there is one implementation of the semantics, two deployments
on two different backends cannot disagree about what a queue does.

Nothing of this exists. The repository held a Go prototype that streamed
messages off a MongoDB tailable capped collection and marked them read the
instant they were sent: no leases, no acknowledgement, no retry, no dead
letters, and a storage layer welded to one database. It proved the idea worth
building and proved nothing else, and has since been deleted — see Impact.

The previous iteration of this project produced a careful contract, a
conformance suite and four crates, and never ran a queue. This change is
scoped so that "done" means a message was enqueued, leased, redelivered after
a failure, dead-lettered on the last attempt, and dequeued on success — over
gRPC, against a storage server the engine reaches only through the SPI.

## What Changes

**BREAKING** — the existing `q.Q` service and the `store.Store` interface are
replaced entirely. Nothing in `q/`, its IDL or `cmd/q/` survives. The
pub/sub service goes with them: it is a gRPC client of `q.Q` and its generated
code carries `q/msg` types, so it cannot outlive the service it consumes.
Whether kyu offers pub/sub again is a question for a later change.

- **Two contracts, one root.** `api/kyu/v1/kyu.proto` is what applications
  call. `api/kyu/storage/v1/storage.proto` is what deployments implement.
  Generated Go code is committed beside the package it belongs to, under
  `kyu/`, which is what Go expects: a
  consumer runs `go get` and builds, with no `buf` and no plugins. CI fails if
  what is committed is not what regeneration produces.

- **The storage SPI is eight operations over opaque items** and names no queue
  concept anywhere in it. It stores items in partitions. Every queue idea —
  message, lease, delivery, dead letter — belongs to the engine and is carried
  into the SPI as argument values, never as vocabulary. The engine supplies
  the clock on every call; storage never reads its own.

- **Two of those eight exist solely to carry an atomicity guarantee.**
  `Claim` finds and holds the first eligible item in one call. `Move`
  relocates an item between partitions or does nothing. Both could be
  expressed as a read followed by a conditional write; both are single calls
  because a guarantee stated inside one signature is the only kind an
  implementer reliably honours.

- **Nothing is deleted except on success.** `Delete` is the only destructive
  operation in the SPI, and the engine calls it only when a consumer
  acknowledges. A message that exhausts its attempts is moved to a dead letter
  partition, never removed.

- **Settling a message names the hold it settles.** `Delete` and `Release` both
  require the caller to state the attempt count it expects, and storage refuses
  the call if the item's count has moved on. Without it a slow consumer
  destroys work another consumer is doing; `specs/storage-spi` carries the
  case.

- **Per-queue policy is durable and opaque.** A partition carries a settings
  value storage stores and returns without reading, so the reaper can recover
  hold duration and attempt ceiling after a restart while no field of the SPI
  names a queue concept.

- **SPI failures are attributable.** Every failure the engine reports that came
  from storage names the operation, the partition and the backend's own status,
  because the backend is written by the deployment and the first question when
  something breaks is which side it broke on.

- **Every queue is created with a companion.** Creating `orders` creates
  `orders.dlq`. Client-supplied names ending in `.dlq` are refused.

- **Consumers get two shapes.** `Dequeue` returns one message and a lease
  token to be acknowledged separately. `StreamingDequeue` is bidirectional:
  the consumer opens with a queue name and an in-flight limit, messages flow
  down, acknowledgements flow up. A broken stream releases its held messages
  at once rather than waiting out their holds.

- **The engine runs a reaper.** One goroutine per queue, spawned at creation
  and at startup, polling for items whose hold has lapsed with attempts
  already spent, and moving them. Jittered and backed off so an idle
  deployment does not flood a customer's storage server with empty queries.

- **A reference storage server ships**, in memory, in Go — so the engine can
  be run and the end-to-end behaviour demonstrated. It is a reference and a
  test fixture, not a production backend.

Explicitly not in this change: the program that judges whether a third-party
storage implementation is adequate; consuming or inspecting dead letter
queues; redrive; priorities; message inspection; retention; idempotency.

## Capabilities

### New Capabilities

- `storage-spi`: the storage contract — eight operations over opaque items,
  the vocabulary rule, single-call atomicity for `Claim` and `Move`, `Delete`
  as the only destructive operation, engine-supplied time, and storage-assigned
  positions.
- `queue-provisioning`: creating a queue and its `.dlq` companion, the
  reserved-suffix rule, and where per-queue settings live.
- `message-enqueue`: appending a message and the ordering guarantee that
  follows from storage-assigned positions.
- `lease-lifecycle`: what makes an item eligible, what `Claim` does to it,
  attempt counting, hold expiry as a non-event, and `Release`.
- `message-dequeue`: the unary and streaming forms, the in-flight limit,
  acknowledgement as the only successful removal, and what a broken stream
  does to messages in flight.
- `dead-letter-transfer`: the attempt ceiling, the move, preservation of the
  attempt count, and the reaper that makes it happen without a consumer
  asking.
- `error-model`: which gRPC status each outcome carries, and the separation of
  "nothing available" from "something is wrong".

### Modified Capabilities

None. `openspec/specs/` is empty.

## Impact

- **Removed**: `q/`, its IDL, `cmd/q/`, and with them `pubsub/`, its IDL and
  `cmd/pubsub/`, which imported them. The
  MongoDB dependency leaves with the queue path, and `go.mod` is left with no
  requirements at all.
- **Replaced**: the `Makefile`, deleted along with the binaries it drove and
  reintroduced by this change with targets for regeneration and for running
  the engine and the reference server.
- **Added**: `api/` as the contract root; a `buf` codegen pipeline; `kyu/`,
  the generated Go code, committed; the engine, the reference storage server
  and their binaries.
- **Untouched**: `LICENSE`, `.gitignore` and the module path.
- **Toolchain**: Go 1.27.1, `buf` 1.50, `protoc` 36.1. The codegen plugins —
  `protoc-gen-go` 1.36.12 and `protoc-gen-go-grpc` 1.6.2 — are pinned by
  `go.mod` tool directives rather than installed globally, so generation
  needs no network and cannot drift between machines. `buf` compiles the
  protos itself; `protoc` is present but is not on the generation path.
- **CI**: two new gates — `buf` breaking-change detection against the previous
  commit for `api/`, and a codegen-drift check that fails if regeneration
  changes the working tree.
- **Anyone deploying kyu** must now run a storage server or write one. There
  is no embedded default.
