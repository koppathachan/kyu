# kyu

A message queue that runs on storage it does not implement.

The queue semantics — ordering, leasing, attempt counting, dead-lettering —
are written exactly once, in an engine kyu ships. Durability is written by
whoever deploys it, behind a gRPC interface they implement themselves or take
from a third party. Because there is one implementation of the semantics, two
deployments on two different backends cannot disagree about what a queue does.

## Status

**The queue runs.** A message can be enqueued, leased, redelivered after a
failure with its attempt count rising, dead-lettered when its attempts run
out, and deleted on acknowledgement — over gRPC, against a storage server the
engine reaches only through the storage interface. Both dequeue forms work and
observe the same semantics. There is a reference in-memory storage server, and
a reaper that transfers exhausted messages with no consumer asking.

The Go/MongoDB prototype that used to live here has been deleted: it marked
messages read the instant it sent them, and had no leases, no acknowledgement,
no retry and no dead letters.

Not here yet: consuming or inspecting a dead letter queue, redrive, priorities,
retention, deduplication, delayed delivery, and any judgement of a third-party
storage implementation. See the non-goals in
[`design.md`](openspec/changes/archive/2026-10-10-build-queue-engine/design.md).

The specification is in [`openspec/specs/`](openspec/specs/), one directory per
capability. The change that produced it — proposal, design and task list — is
archived in
[`openspec/changes/archive/2026-10-10-build-queue-engine/`](openspec/changes/archive/2026-10-10-build-queue-engine/).

## Architecture

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

- **Applications** call `kyu.v1.Kyu`: `CreateQueue`, `Enqueue`, `Dequeue`,
  `StreamingDequeue`, `Ack`.
- **The engine** (`cmd/kyu`) implements every queue rule and reads the only
  clock in the system. It keeps no durable state, so any number of replicas
  can share one storage server, and a lease granted by one can be acknowledged
  through another. Each replica runs a reaper that moves exhausted messages to
  their dead letter queue.
- **The storage server** implements `kyu.storage.v1.Storage`: eight
  operations over opaque items in partitions. The deployment supplies it, and
  it is given every queue decision as an argument rather than asked to make
  one.

Neither contract is a library: both are gRPC, so either side can be written in
any language.

The detailed version — packages, the engine's internals, each request's path
through the system, concurrency, and what happens when each part fails — is in
[`docs/architecture.md`](docs/architecture.md).

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
buf.yaml                 module, lint and breaking-change configuration
buf.gen.yaml             what is generated, and with which plugins

api/                     the contract root, and the sole proto path
  kyu/v1/kyu.proto               what applications call
  kyu/storage/v1/storage.proto   what deployments implement

kyu/                     generated Go, committed and kept in step
  v1/  storage/v1/

internal/                the implementation; importable by nobody
  queue/                 what a queue is: settings, lease token, names, ordering
  spi/                   the only reach into storage: client, errors
  reaper/                dead letter transfer: sweep policy, sweeper
  engine/                the kyu.v1.Kyu service, and its configuration
  storage/memory/        the reference storage server — a fixture, not a backend

cmd/kyu/                 the engine binary
cmd/kyu-memstore/        the reference storage server binary
build/package/           a Dockerfile per binary
init/                    the systemd unit and its environment file, for the Linux packages
scripts/                 build scripts
.github/workflows/       CI, and the release a tag triggers
.goreleaser.yaml         what a release builds and publishes

docs/
  architecture.md        how kyu is built, in detail

openspec/
  config.yaml            the layout and conventions above, for this workflow
  specs/<capability>/    accepted requirements and scenarios, one per capability
  changes/archive/
    2026-10-10-build-queue-engine/
      proposal.md        why the change existed and what it changed
      design.md          the decisions, their alternatives, the diagrams
      tasks.md           the implementation checklist, all done
```

The directory under `api/` mirrors the proto package exactly, so a package
name and a file path cannot disagree, and generated code lands beside the
package it belongs to: `api/kyu/v1/kyu.proto` produces `kyu/v1/kyu.pb.go`. It
is committed, as Go expects — `go get` and `go build` need nothing else — and
rebuilt by `make generate`; `make generate-check`, which CI runs, fails if what
is committed is not what regeneration produces.

Each package under `internal/` is a domain rather than a layer. `queue` holds
the vocabulary and performs no I/O at all, so the rules the engine is built
from can be checked without a server. Only the generated packages are public:
the reference storage server is a fixture, and a fixture other people can
import becomes a dependency.

Every `_test.go` file declares `package <name>_test` and exercises its package
through the exported API. The engine's tests drive it through `kyu.v1.Kyu` and
read results through `kyu.storage.v1.Storage` — the two things a deployment
actually has.

For how the system works, read [`docs/architecture.md`](docs/architecture.md).
For why it was built this way, read the archived `proposal.md` and then
`design.md`, which carries the arguments and the alternatives rejected. For the
requirements themselves, read the capability specs.

## Deploying it

**A deployment must supply a storage server.** The engine embeds no backend and
there is no default: `kyu` refuses to start without `-storage` or
`KYU_STORAGE`. What a deployment implements is `kyu.storage.v1.Storage`, whose
eight operations are listed above and specified in full in
[`api/kyu/storage/v1/storage.proto`](api/kyu/storage/v1/storage.proto) —
the comments there are the contract, not a summary of it. It is gRPC, so it can
be written in any language.

**The reference server in `cmd/kyu-memstore` is not such a backend.** It
holds everything in process memory: stop it and every message is gone. It
exists so that the contract has a worked example and so that kyu's own tests
run against a real backend over a real socket.

## Installing it

Every release is a tag, `v0.1.0` and on, published on the
[releases page](https://github.com/koppathachan/kyu/releases). Each one
carries the engine, `kyu`, and the reference storage server, `kyu-memstore`.

| You want | Get it with |
|---|---|
| A container | `docker pull ghcr.io/koppathachan/kyu` — and `ghcr.io/koppathachan/kyu-memstore` — for `linux/amd64` and `linux/arm64` |
| macOS or Linux, via Homebrew | `brew install --cask koppathachan/tap/kyu` |
| Debian or Ubuntu | the `.deb` from the release: `sudo apt install ./kyu_<version>_linux_amd64.deb` |
| Fedora, RHEL, Amazon Linux | the `.rpm` from the release: `sudo dnf install ./kyu_<version>_linux_amd64.rpm` |
| Alpine | the `.apk` from the release: `sudo apk add --allow-untrusted ./kyu_<version>_linux_amd64.apk` |
| A plain binary | the `.tar.gz` for your platform from the release, checked against `checksums.txt` |
| From source | `go install github.com/koppathachan/kyu/cmd/kyu@latest` |
| The generated clients, in Go | `go get github.com/koppathachan/kyu@latest`, then import `kyu/v1` or `kyu/storage/v1` |

The Linux packages install `kyu` with a systemd unit that is not enabled,
because kyu has no default backend. Set `KYU_STORAGE` in `/etc/kyu/kyu.env`,
then `sudo systemctl enable --now kyu`. `kyu-memstore` is packaged separately
and has no unit: it keeps everything in memory, so it is for trying kyu out.

`kyu -version` reports the release a binary was built from.

## Running it

Building and running need Go 1.27 and nothing else, because the generated
code is committed:

```sh
make build        # build every binary into bin/
make test         # run the tests, with the race detector
make help         # every target
```

### A queue from a cold start

Two processes. In one terminal, the reference storage server:

```sh
make run-memstore   # kyu.storage.v1.Storage on 127.0.0.1:50052
```

(`make -j2 run` starts both at once, once you no longer want them separate.)

In another, the engine pointed at it. A short sweep interval is used here so
that a dead letter transfer happens while you are watching; the default floor
is a second, and it backs off to thirty:

```sh
KYU_SWEEP_INTERVAL=200ms make run-kyu   # kyu.v1.Kyu on 127.0.0.1:50051
```

Both servers have reflection enabled, so `grpcurl` can drive them without a
copy of the contract. Create a queue that holds a message for five seconds and
gives it two attempts:

```sh
grpcurl -plaintext -d '{"queue":"orders","hold_duration":"5s","attempt_ceiling":2}' \
  127.0.0.1:50051 kyu.v1.Kyu/CreateQueue
```

That created `orders` and `orders.dlq`. Enqueue something — the payload is
bytes, so it is base64 on the wire:

```sh
grpcurl -plaintext -d '{"queue":"orders","payload":"c2hpcCBpdA=="}' \
  127.0.0.1:50051 kyu.v1.Kyu/Enqueue
{
  "id": "1339816f09ff7c8abb24b42fd2168a11",
  "position": "1"
}
```

Take it, and then do nothing at all:

```sh
grpcurl -plaintext -d '{"queue":"orders"}' 127.0.0.1:50051 kyu.v1.Kyu/Dequeue
{
  "message": {
    "id": "1339816f09ff7c8abb24b42fd2168a11",
    "payload": "c2hpcCBpdA==",
    "attempt": 1,
    "leaseToken": "AQAAAAEABm9yZGVyczEzMzk4MTZmMDlmZjdjOGFiYjI0YjQyZmQyMTY4YTEx"
  }
}
```

Five seconds later the hold has lapsed, and the same message comes back with
its attempt count risen. Nothing observed the hold passing: the message simply
became eligible again.

```sh
grpcurl -plaintext -d '{"queue":"orders"}' 127.0.0.1:50051 kyu.v1.Kyu/Dequeue
{
  "message": {
    "id": "1339816f09ff7c8abb24b42fd2168a11",
    "payload": "c2hpcCBpdA==",
    "attempt": 2,
    "leaseToken": "AQAAAAIABm9yZGVyczEzMzk4MTZmMDlmZjdjOGFiYjI0YjQyZmQyMTY4YTEx"
  }
}
```

That was attempt two of two. Let this hold lapse as well, and the message is
undeliverable from the moment it is spent — no consumer can have it, whether or
not anything has moved it yet:

```sh
grpcurl -plaintext -d '{"queue":"orders"}' 127.0.0.1:50051 kyu.v1.Kyu/Dequeue
{}
```

Nobody asks for anything from here on. The reaper moves it to `orders.dlq` on
its own. How soon depends on the sweep interval and the backlog, and no timing
is promised — so if the partition looks empty, look again. Read it at the
storage server, since nothing in `kyu.v1.Kyu` consumes a dead letter queue yet:

```sh
grpcurl -plaintext -d '{"partition":"orders.dlq","now":"9999999999999999","attempt_floor":0,"limit":10}' \
  127.0.0.1:50052 kyu.storage.v1.Storage/Scan
{
  "items": [
    {
      "partition": "orders.dlq",
      "id": "1339816f09ff7c8abb24b42fd2168a11",
      "position": "1",
      "value": "c2hpcCBpdA==",
      "attempts": 2
    }
  ]
}
```

Same id, same payload, both attempts still recorded, and no `Delete` was issued
for it anywhere along the way.

Now the ordinary path. Enqueue another, take it, and acknowledge it with the
lease token the delivery carried:

```sh
grpcurl -plaintext -d '{"lease_token":"AQAAAAEABm9yZGVyczY0YTE1MzQ3MTJiYjkyMDVkMzVlNGM5YzlmYjcxYjZi"}' \
  127.0.0.1:50051 kyu.v1.Kyu/Ack
{}
```

It is gone, and the queue is empty. Send that same acknowledgement a second
time and the engine refuses it rather than pretending:

```sh
ERROR:
  Code: Aborted
  Message: message "64a1534712bb9205d35e4c9c9fb71b6b" in queue "orders" was not settled:
           the message is no longer this consumer's to settle
```

`ABORTED` is the honest answer: nothing in the storage interface distinguishes
this consumer's own earlier settlement from a message the reaper moved out from
under it, so the engine does not claim to know which happened. A consumer told
`ABORTED` should assume its work may need doing again — the queue is
at-least-once, and that was already true of it.

### Changing a contract

Changing a `.proto` additionally needs [`buf`](https://buf.build); the codegen
plugins are pinned by `go.mod` tool directives, so there is nothing else to
install.

```sh
make generate         # rebuild kyu/ from api/
make generate-check   # fail if kyu/ is not what regeneration produces
make lint-proto       # lint the contracts, and check their formatting
make breaking         # fail if api/ breaks the contract published on main
```

[CI](.github/workflows/ci.yml) runs all four on every push to `main` and every
pull request, along with `gofmt`, `go mod tidy`, `go vet`, the tests with
`-race`, and both container image builds. On a pull request `breaking`
compares against the pull request's base; on a push, against the previous tip
of `main`. A break that is intended is declared rather than hidden: a commit
carrying a `Breaking-Change: <why>` trailer skips that comparison, and CI
prints the reason.

Container images are built per binary from `build/package/`:

```sh
make docker-image
```

### Cutting a release

```sh
git tag v0.1.0
git push origin v0.1.0
```

The [release workflow](.github/workflows/release.yml) runs all of CI, then
[GoReleaser](.goreleaser.yaml) builds the archives, checksums, Linux packages
and Homebrew cask and publishes the GitHub Release, the images go to
`ghcr.io`, and the Go module proxy is asked for the tag. A tag with a suffix,
such as `v0.2.0-rc.1`, is published as a prerelease and does not move
`latest`. `make release-snapshot` builds everything into `dist/` locally
without publishing; it needs [`goreleaser`](https://goreleaser.com).

## License

MIT. See [LICENSE](LICENSE).
