## 1. Repository layout, and removing the prototype

- [ ] 1.1 Create `spec/` as the contract root, with the directory path under it mirroring the proto package exactly, so `spec/` is the sole `--proto_path` and a package name and a file path never disagree
- [x] 1.2 Delete `q/`, `proto/q/` and `cmd/q/` in one commit, and drop the MongoDB driver from `go.mod`. The prototype shares no interface with what replaces it and marking messages read on send is the behaviour being fixed
- [x] 1.3 Delete `pubsub/`, `proto/pubsub/` and `cmd/pubsub/` in the same commit — pub/sub imports `q` and `q/msg` and cannot outlive them — along with `scripts/` and the `Makefile`, which drove the deleted binaries, and rewrite `README.md` from the specs so the repository describes what is being built rather than what was deleted
- [ ] 1.4 Add `.gitignore` entries for generated Go code — Go is the only language this change generates into — and verify nothing generated is committed

## 2. The storage contract — `spec/kyu/storage/v1/storage.proto`

- [ ] 2.1 Declare `package kyu.storage.v1` and the `Storage` service with exactly the eight methods, and no more
- [ ] 2.2 Define `Item` with exactly `partition`, `id`, `position`, `value`, `hold_until` and `attempts`
- [ ] 2.2a Define a partition as a name plus an opaque settings value, with a comment saying storage stores and returns it unread — this is what lets queue policy be durable without any SPI field naming a queue concept
- [ ] 2.3 Define `CreatePartition` and `ListPartitions`, with partition settings carried on both so the reaper can read back what creation recorded
- [ ] 2.4 Define `Put` taking the caller's id and returning the position storage assigned, with `ALREADY_EXISTS` on a duplicate id and `NOT_FOUND` on a partition that does not exist — no implicit partition creation
- [ ] 2.5 Define `Claim` taking partition, `now`, `hold_until` and attempt ceiling, and returning an optional item — with the indivisibility of select-hold-increment stated in the comment as a requirement on the implementation, not a hint
- [ ] 2.6 Define `Release`, `Delete` and `Move`. `Release` and `Delete` take a required expected attempt count. `Move` takes none — not because the message is safe, but because no fence helps: attempts do not change while a message is held, so the reaper cannot distinguish a lapsed hold from a consumer still working under one. State `Move`'s all-or-nothing requirement in its comment, and do not claim in it that the transfer is race-free
- [ ] 2.7 Define `Scan` taking partition, instant, attempt floor and limit, and returning items — with a comment saying plainly that it is called on a timer for every partition and will usually return nothing, so an implementer knows what they are optimising for
- [ ] 2.8 Write the normative comments. With no conformance program in this change, the proto is the whole of what a customer receives: every atomicity, ordering and clock requirement in `specs/storage-spi` must be stated in the file itself
- [ ] 2.9 Verify against `specs/storage-spi` that no identifier anywhere in the file names a queue, message, lease, delivery, acknowledgement or dead letter
- [ ] 2.10 State in the comments on `Delete` and `Release` that the expected attempt count is not an optimisation and not optional — a backend that ignores it lets a slow consumer destroy a message another consumer is processing, and nothing upstream can detect that it did

## 3. The client contract — `spec/kyu/v1/kyu.proto`

- [ ] 3.1 Declare `package kyu.v1` and the `Kyu` service with `CreateQueue`, `Enqueue`, `Dequeue`, `StreamingDequeue` and `Ack`
- [ ] 3.2 Define `CreateQueue` carrying hold duration and attempt ceiling, both optional and defaulted
- [ ] 3.3 Define `Enqueue` returning the message id and the position
- [ ] 3.4 Define `Dequeue` returning an optional message and a lease token, and `Ack` taking that token. `Ack` fails with `ABORTED` when the hold has lapsed and the message has passed on — not `FAILED_PRECONDITION`, which a client receives for an unreadable queue, and not `OK`, which would tell a consumer it settled a message someone else is still processing
- [ ] 3.5 Define `StreamingDequeue` with the client stream as a `oneof` of `Open` and `Ack`, and the server stream as a `oneof` of a message and a settlement rejection naming a message id. The rejection is what keeps the streaming form's semantics identical to the unary form's `ABORTED`. `Open` carries the queue and the in-flight limit and nothing else — hold duration is the queue's setting, deliberately not per-call, so the two forms cannot diverge
- [ ] 3.6 Confirm this file imports nothing from `kyu.storage.v1`. The two contracts are independent; sharing a type would make an engine-side change a storage-side breaking change

## 4. Codegen

- [ ] 4.1 Add `buf.yaml` and `buf.gen.yaml` rooted at `spec/`, pinning plugin versions through `go.mod` tool directives
- [ ] 4.2 Add breaking-change detection against the previous commit for `spec/`, so a change to a published package is caught mechanically rather than by review
- [ ] 4.3 Add a `Makefile` target that regenerates and fails if the working tree changes, so a stale generation cannot be committed
- [ ] 4.4 Generate both packages and confirm the storage server interface and the client stubs compile

## 5. Reference storage server

- [ ] 5.1 Implement an in-memory `kyu.storage.v1.Storage` server, per-partition ordered by position, with a single mutex per partition
- [ ] 5.2 Implement `Claim` as one critical section covering selection, hold and increment, so the indivisibility requirement holds by construction rather than by care
- [ ] 5.3 Implement `Move` as one critical section over both partitions, taking locks in a defined order so two concurrent moves between the same pair cannot deadlock
- [ ] 5.4 Assign positions from a per-partition counter incremented inside the write's critical section — not from a clock, which is the failure `specs/storage-spi` names as hardest to observe
- [ ] 5.5 Confirm nothing in it reads a clock, and that no code path deletes an item except `Delete`
- [ ] 5.6 Give it a `cmd/` binary and a listen address, so the engine reaches it over real gRPC rather than in-process
- [ ] 5.7 Label it in its package documentation as a reference and a test fixture, not a production backend
- [ ] 5.8 Implement the attempt-count fence on `Delete` and `Release` inside the same critical section as the read that checks it, and test it directly: a settlement naming a stale attempt count is refused with `FAILED_PRECONDITION` and changes nothing
- [ ] 5.9 Implement `Put`'s duplicate-id and missing-partition refusals, and `Move`'s refusal when the destination already holds that id

## 6. Engine — provisioning and enqueue

- [ ] 6.1 Build the engine's storage client: a thin wrapper over the generated stubs holding the connection and nothing else, with no backend-specific behaviour anywhere in it
- [ ] 6.2 Implement `CreateQueue` issuing two `CreatePartition` calls with the same settings value, failing the whole call if the companion cannot be created
- [ ] 6.2a Implement the settings codec: encode hold duration and attempt ceiling into the opaque value on the way out, decode on the way back from `ListPartitions`. Never fall back to defaults on a value that will not decode — the whole point of storing the ceiling is that guessing it risks delivering past a limit
- [ ] 6.2b Implement queue discovery: on a client call naming a queue this engine holds no settings for, call `ListPartitions` before answering — adopt the settings and start a sweep if it is there and decodable, apply 6.2c if it is there and not, fail with `NOT_FOUND` if it is absent. Never infer absence from engine memory; a queue created a moment ago on another replica is real. Add a negative cache with a short TTL from `KYU_NEGATIVE_CACHE_TTL`, and test that an absence expires — a client told `NOT_FOUND`, a create on another replica, a retry after the TTL, served
- [ ] 6.2c Implement the undecodable-settings outcome: refuse to serve that queue with `FAILED_PRECONDITION`, start no sweep for it, report it by name, and keep serving every other queue. One queue written by a newer engine must not take the deployment down
- [ ] 6.3 Enforce the reserved `.dlq` suffix on every client-supplied queue name, in `CreateQueue` and `Enqueue`
- [ ] 6.4 Implement `Enqueue`, assigning the message id engine-side and returning storage's position
- [ ] 6.5 Add the position-monotonicity check at `Put`, not at `Claim`: record the highest position already completed for that partition when a `Put` is issued, and report a violation if the response comes back lower, naming the backend, the partition and both positions. The high-water mark is in-memory and best-effort — it resets on restart, and no SPI call is to be added to make it durable. Report and continue serving; taking a queue out of service converts an ordering fault we cannot fix into an outage we caused. A check at `Claim` would be wrong: redelivery legitimately returns a lower position than messages claimed since, so it would fire on every retry, including in task 11.1
- [ ] 6.6 Tests: both partitions created; reserved names refused; `ALREADY_EXISTS` on a repeat; settings read back through `ListPartitions` after a restart

## 7. Engine — lease and unary dequeue

- [ ] 7.1 Implement `Dequeue` as a single `Claim` with the queue's ceiling, returning an empty result when nothing is eligible
- [ ] 7.2 Implement the lease token: partition, message id and the attempt count the claim returned — opaque to the client and useless for anything but `Ack`. The attempt count travels in the token rather than in engine memory, which is what makes an acknowledgement safe across a restart and across engine instances
- [ ] 7.3 Implement `Ack` as a `Delete` fenced on the token's attempt count. Both refusals — `FAILED_PRECONDITION` when the hold passed to another consumer, `NOT_FOUND` when the reaper already moved the message to `.dlq` — are ordinary for the engine and are returned to the consumer as `ABORTED`. Neither is retried and neither is reported as a fault. Confirm no other path in the engine issues a `Delete`
- [ ] 7.4 Implement hold duration resolution — the queue's setting, else the default — computed against the engine's clock and passed as `hold_until`. There is no per-dequeue override; adding one to the unary form only would break the identical-forms rule
- [ ] 7.5 Tests: a held message is invisible to a second consumer; it reappears after its hold; attempts increment once per claim and survive an engine restart; an unacknowledged message is never deleted
- [ ] 7.6 Test the slow-consumer case end to end: A claims, A's hold lapses, B claims, A then acknowledges — assert A's delete is refused, A is told `ABORTED`, the message is still present, and B's hold stands. Run it again with the engine restarted between A's claim and A's acknowledgement, which is the case engine memory cannot cover
- [ ] 7.7 Test the other lost-settlement branch, the one recorded under Risks: a consumer holds a message at the attempt ceiling, its hold lapses, the reaper moves the message to `.dlq`, and the consumer then acknowledges — assert the `Delete` returns `NOT_FOUND`, the consumer is told `ABORTED` rather than `OK`, and the message is in `.dlq` exactly once

## 8. Engine — streaming dequeue

- [ ] 8.1 Implement `StreamingDequeue`: require `Open` first, reject a second `Open`, terminate a malformed stream with `INVALID_ARGUMENT`
- [ ] 8.2 Implement the in-flight window: claim up to the limit, send one more per acknowledgement received, never exceed it
- [ ] 8.3 Handle acknowledgements arriving in any order, and ignore one naming a message this stream does not hold. When a `Delete` is refused either way, send a settlement rejection naming that message rather than staying silent — the streaming form has to tell the consumer what the unary form tells it with `ABORTED` — and free the in-flight slot regardless, or a consumer whose holds have all lapsed stalls at its limit on an open stream
- [ ] 8.4 On stream end by any cause, `Release` every message the stream held and had not acknowledged, each fenced on the attempt count from its claim, treating `FAILED_PRECONDITION` as ordinary — the hold had already lapsed and passed on
- [ ] 8.5 Tests: a consumer that never acknowledges receives exactly its limit; out-of-order acknowledgement settles the right message; a broken stream returns its messages at once; acknowledged messages are not returned; a stream breaking after its holds already lapsed does not disturb the consumer that has since claimed them
- [ ] 8.6 Test that the unary and streaming forms agree: run one sequence of operations through each and assert the observable outcomes match

## 9. Engine — dead letter transfer and the reaper

- [ ] 9.1 Pass the queue's attempt ceiling on every `Claim`, so a message at the ceiling is undeliverable before any transfer occurs — this is what removes the reaper from the correctness path. It does not remove the race between the reaper and a consumer, which is not closable here; see the risk in `design.md`, and do not write the opposite into any comment
- [ ] 9.2 Implement the transfer: `Move` to `<queue>.dlq`, preserving id, payload and attempt count, issuing no `Delete`. Treat `ALREADY_EXISTS` from the destination as a defect to report, not an ordinary outcome — it means two messages share an id
- [ ] 9.3 Implement the per-queue sweep: `Scan` for lapsed holds with attempts at or above the ceiling, with an explicit limit defaulting to 100, and `Move` each result. Never send `Scan` without a limit — the backend would be choosing the reaper's throughput
- [ ] 9.4 Start a sweep at `CreateQueue`, at startup for every partition from `ListPartitions`, and on discovery for any partition a later enumeration returns that is not already swept; start none for a partition ending in `.dlq`. Re-enumerate on a timer — without it, a queue created on another replica is swept by nobody once that replica goes away
- [ ] 9.5 Implement jitter on the initial offset and exponential backoff between the configured floor and ceiling, resetting to the floor on any non-empty scan
- [ ] 9.6 Read `KYU_SWEEP_INTERVAL`, `KYU_SWEEP_INTERVAL_MAX`, `KYU_SCAN_LIMIT`, `KYU_DISCOVERY_INTERVAL` and `KYU_NEGATIVE_CACHE_TTL`, with flag overrides
- [ ] 9.7 Treat `NOT_FOUND` from `Move` as an ordinary outcome and continue
- [ ] 9.8 Tests: a message reaching the ceiling is moved with no consumer present; sweeps resume after restart; no sweep runs for a `.dlq`; correctness holds with the reaper disabled — no message is delivered past its ceiling; two engines sweeping one queue leave the message present exactly once
- [ ] 9.9 Test discovery across replicas: create a queue through engine B, then enqueue to it through engine A, which has never seen it — A serves the call and begins sweeping it. Then stop B and confirm A still transfers that queue's exhausted messages, which is the case startup-only enumeration silently failed

## 10. Error model

- [ ] 10.1 Implement the status table from `specs/error-model` across both services, and confirm no call returns a status for an outcome the table does not name it for
- [ ] 10.2 Confirm every empty result — `Claim`, `Scan`, `Dequeue` — returns `OK`, and that no path returns `NOT_FOUND` for emptiness
- [ ] 10.3 Surface `UNAVAILABLE` and `INTERNAL` to the caller without retrying, and keep transport statuses — `CANCELLED`, `DEADLINE_EXCEEDED` — out of the outcome table and out of fault reporting
- [ ] 10.4 Include the operation, the partition and the backend's own status in every SPI-originated failure the engine reports
- [ ] 10.5 Tests: an unreachable backend produces one reported failure and no retry; a status named in the table is never reused for an outcome it does not name; a cancelled stream is not reported as a fault. The handling of `FAILED_PRECONDITION` and of `NOT_FOUND` from `Move` is tested where it is implemented, in 7.6, 8.5 and 9.8 — do not re-test it here

## 11. Demonstrating it

- [ ] 11.1 Write the end-to-end test that this change exists to make pass: create a queue, enqueue, claim, fail by letting the hold lapse, observe redelivery with the attempt count rising, reach the ceiling, observe the message arrive in `.dlq` without a consumer asking, then enqueue another, consume it and acknowledge it, and confirm it is gone
- [ ] 11.2 Run that test through both the unary and the streaming form
- [ ] 11.3 Run it against the reference storage server over gRPC on a real socket, never in-process
- [ ] 11.4 Add `make run` targets for the engine and the reference storage server, and a README section showing a queue being driven end to end from a cold start
- [x] 11.5 Record in the README that a deployment must supply a storage server, that the reference in-memory one is not one, and that `kyu.storage.v1.Storage` is what a deployment implements
