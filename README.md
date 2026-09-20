# kyu

A message queue that runs on storage it does not implement.

The queue semantics — ordering, leasing, attempt counting, dead-lettering —
are written exactly once, in an engine kyu ships. Durability is written by
whoever deploys it, behind a gRPC interface they implement themselves or take
from a third party. Because there is one implementation of the semantics, two
deployments on two different backends cannot disagree about what a queue does.

## Status

**Nothing is implemented yet.** This repository currently holds the
specification for the first change and no code. The Go/MongoDB prototype that
used to live here has been deleted: it marked messages read the instant it
sent them, and had no leases, no acknowledgement, no retry and no dead
letters.

The specification is in [`openspec/changes/build-queue-engine/`](openspec/changes/build-queue-engine/).
"Done" for that change means a message has been enqueued, leased, redelivered
after a failure, dead-lettered on its last attempt and acknowledged on
success — over gRPC, against a storage server the engine reaches only through
the storage interface.

## The shape of it

Two contracts, and one component in the middle that knows what a queue is:

```text
  ┌──────────────────────────────────────────────────────────────┐
  │ applications                                                 │
  │   Enqueue · Dequeue · Ack · StreamingDequeue                 │
  └──────────────────────────────┬───────────────────────────────┘
                                 │  kyu.v1.Kyu — queues, messages,
                                 │  leases, acks, dead letters
                                 ▼
  ┌──────────────────────────────────────────────────────────────┐
  │ engine                                                       │
  │   queue semantics, written once                              │
  │   the only clock in the system                               │
  │   reaper — one goroutine per queue                           │
  └──────────────────────────────┬───────────────────────────────┘
                                 │  kyu.storage.v1.Storage — partitions,
                                 │  items, positions, holds, attempts
                                 ▼
  ┌──────────────────────────────────────────────────────────────┐
  │ storage server — supplied by the deployment                  │
  │   eight operations · no clock · nothing that names a queue   │
  └──────────────────────────────────────────────────────────────┘
```

`kyu.v1.Kyu` is what applications call: `CreateQueue`, `Enqueue`, `Dequeue`,
`StreamingDequeue`, `Ack`. `kyu.storage.v1.Storage` is what a deployment
implements. Neither is a library: both are gRPC, so a backend can be written
in any language.

## What a queue does

- **A message is delivered only under a hold.** Dequeuing claims a message
  and holds it for the queue's hold duration; no other consumer sees it while
  the hold stands.
- **A lapsed hold is not an event.** Nothing watches it. A message whose hold
  has passed is simply eligible again the next time somebody claims, so
  correctness does not depend on any background process running.
- **Every claim counts as an attempt.** The count lives in storage and
  nowhere else, so it survives an engine restart and cannot drift.
- **Acknowledgement is the only thing that deletes a message.** Nothing else
  in the system destroys one.
- **A message that exhausts its attempts is moved, not dropped.** Creating
  `orders` also creates `orders.dlq`, and an exhausted message is moved there
  with its id, payload and attempt count intact. Names ending in `.dlq` are
  reserved.
- **Settling names the hold it settles.** Acknowledging carries the attempt
  count the claim returned, and storage refuses the call if the count has
  moved on — so a consumer that is slow rather than dead cannot destroy a
  message another consumer has since claimed. It is told `ABORTED` instead of
  a false `OK`.
- **Delivery is at-least-once.** A consumer that finishes work and dies
  before acknowledging will see its message again. Exactly-once is not
  offered.
- **Nothing available is a result, not an error.** An empty queue returns
  success carrying no message.

Consumers get two shapes. `Dequeue` returns one message and a lease token,
acknowledged by a separate `Ack`. `StreamingDequeue` is bidirectional: the
consumer opens with a queue name and an in-flight limit, messages flow down,
acknowledgements flow up, and a broken stream releases what it held at once
rather than waiting out the holds. Both observe identical semantics.

## The storage interface

Eight operations over opaque items in partitions. Storage has no idea one of
its partitions is a queue:

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

Three rules shape the whole contract. **No identifier in it names a queue
concept** — not a queue, a message, a lease, a delivery, an acknowledgement
or a dead letter; every queue idea arrives as an argument value instead.
**Storage never reads a clock**: every time-dependent call receives the
instant as an argument, so two components can never disagree about whether a
hold has lapsed. **`Claim` and `Move` are single calls** because a guarantee
stated inside one signature is the only kind an implementer reliably honours.

## Defaults

| Setting | Default | Where |
|---|---|---|
| hold duration | 30s | per queue, at creation |
| attempt ceiling | 5 | per queue, at creation |
| in-flight limit | 1 | per stream, when it opens |
| sweep floor | 1s | `KYU_SWEEP_INTERVAL` |
| sweep ceiling | 30s | `KYU_SWEEP_INTERVAL_MAX` |
| scan limit | 100 | `KYU_SCAN_LIMIT` |
| discovery interval | 30s | `KYU_DISCOVERY_INTERVAL` |
| negative cache TTL | 5s | `KYU_NEGATIVE_CACHE_TTL` |

## Repository layout

```text
openspec/
  changes/build-queue-engine/
    proposal.md          why this change exists and what it changes
    design.md            the decisions, their alternatives, the diagrams
    specs/<capability>/  requirements and scenarios, one per capability
    tasks.md             the implementation checklist
  specs/                 accepted specifications (empty until the first archive)
```

Read `proposal.md` first, then `design.md` — the design carries the arguments
and the diagrams — then the capability specs for the requirements themselves.

Planned, and not yet present: `spec/` as the contract root, holding
`spec/kyu/v1/kyu.proto` and `spec/kyu/storage/v1/storage.proto`; the engine;
a reference in-memory storage server; and their binaries. Anyone deploying
kyu will need to run a storage server or write one — there is no embedded
default.

## License

MIT. See [LICENSE](LICENSE).
