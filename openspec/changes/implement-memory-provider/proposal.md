## Why

`crates/kyu-memory/` is empty: the Kyu v1 storage provider SPI has a contract (`kyu-conformance`) and an acceptance suite (thirteen §8.1 scenarios) but no implementation to run them against. Without a working in-memory provider there is no reference implementation of the SPI, and nothing that proves the conformance suite is satisfiable.

## What Changes

- Implement `kyu_memory::MemoryProvider`, an in-memory `kyu_conformance::StorageProvider`, so that all thirteen §8.1 scenarios pass.
- Durable-for-the-process queue registry: `EnsureQueue` (idempotent, never conflicts), `GetQueueConfig`, `ListQueues` (ordered, paged), `DeleteQueue` (purges and counts), `GetQueueStats`.
- Message lifecycle: `SaveMessage` with priority, visibility delay, attributes, and `(queue_id, idempotency_key)` deduplication whose records outlive the messages they name.
- Delivery: `LeaseMessages` selecting in `priority DESC, visible_at ASC, message_id ASC` order, atomic under concurrent callers.
- Leases: fresh token per delivery, `RenewLease` that does not rotate, and final expiry — any stale or expired token fails `LEASE_LOST` without touching the message.
- Settlement: `SettleMessages` as a per-item batch that never fails the whole RPC for item-level problems.
- Dead-lettering driven by `max_deliveries`, reachable via nack or lease expiry, with the `dead_letter_enabled = false` drop path and forced dead-letter below the limit.
- Self-reaping of expired leases with no external trigger, plus `ReapExpiredLeases` as an administrative sweep that reports consistent counts.
- `GetProviderInfo` reporting `contract_version = "kyu.storage.v1"`.
- §4 limits enforced with **one documented exception**: the `lease_ttl` / `RenewLease.extension` **minimum of 1 s is not enforced**, because §8.1 scenarios r03, r04, r06 and r12 lease with a 300 ms TTL. §4's table is written in §5's client-API vocabulary (`Dequeue.batch_size`, `Settle.settlements`) and binds a provider only by analogy; where that analogy contradicts the fixed oracle, §8.1 wins. The 12 h maximum is still enforced.

Not breaking: nothing exists to break.

## Capabilities

### New Capabilities

- `queue-registry`: queue creation, lookup, listing, deletion, statistics, and the isolation guarantee that messages never cross queues.
- `message-enqueue`: saving messages with payload, attributes, priority and visibility delay; idempotency-key deduplication and its window.
- `message-delivery`: leasing messages in §3.2 order, `delivery_count` accounting, and atomicity under N concurrent leasers.
- `lease-lifecycle`: lease token issue and rotation, renewal, expiry finality, and per-item batch settlement (ack / retry / forced dead-letter).
- `dead-lettering`: `max_deliveries` enforcement, the `DEAD_LETTER` terminal state, the disabled-DLQ drop path, and forced dead-lettering.
- `provider-maintenance`: self-reaping of expired leases, the `ReapExpiredLeases` administrative trigger, and provider identity.

### Modified Capabilities

None. `openspec/specs/` is empty; every capability above is new.

## Impact

- **New code**: `crates/kyu-memory/src/` only.
- **Dependencies**: none added. `kyu-memory` stays on `std` alone — no async runtime, no UUID crate (message ids are a zero-padded monotonic counter, which satisfies §3.2's lexicographic-ordering tiebreak that §2 recommends UUIDv7 for).
- **Untouched, by rule**: `crates/kyu-conformance/` and `crates/kyu-memory/tests/conformance.rs` are a fixed oracle.
- **Public API**: `MemoryProvider::new()` implementing `kyu_conformance::StorageProvider`, per the fixed entry point.
- **Out of scope**: the queue engine (§5, §8.2), any non-memory provider, the gRPC binding, and every §9 non-goal.
- **Verification**: `cargo test -p kyu-memory --features conformance` is the acceptance gate; `cargo clippy --all-targets -- -D warnings` must be clean.
