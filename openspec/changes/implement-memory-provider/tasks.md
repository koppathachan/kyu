## 1. Skeleton and state

- [x] 1.1 Add `kyu-conformance` as a normal dependency of `kyu-memory` (it is currently only a dev-dependency, but `MemoryProvider` must implement its trait); confirm no other dependency is added
- [x] 1.2 Create the module layout under `crates/kyu-memory/src/`: `lib.rs` (public `MemoryProvider`), `state.rs` (`State`, `Queue`, `Message`, `DedupRecord`), `sweep.rs`, `validate.rs`
- [x] 1.3 Define `State { queues: HashMap<String, Queue>, next_id: u64 }` and `Queue { config, messages: HashMap<String, Message>, dedup: HashMap<String, DedupRecord> }` per design D1
- [x] 1.4 Define `Message` with an explicit state enum (`Ready`/`Leased`/`DeadLetter`), `visible_at: Instant`, `delivery_count: i32`, `lease_token: Option<String>`, `lease_expires_at: Option<Instant>`, `dead_letter_reason: String`, plus payload, priority and attributes
- [x] 1.5 Implement `MemoryProvider::new()` and a private `lock()` helper using `unwrap_or_else(PoisonError::into_inner)` per design D1
- [x] 1.6 Implement `next_message_id(&mut State) -> String` as `format!("{:020}", …)` over the monotonic counter, per design D6
- [x] 1.7 Add `Instant`-to-`SystemTime` conversion helpers taking a single `now` captured at method entry, per design D7
- [x] 1.8 Stub every `StorageProvider` method with `todo!()` so the crate compiles and `cargo test -p kyu-memory --features conformance` runs (and fails) end to end

## 2. Method shape and the sweep chokepoint

- [x] 2.1 Write `sweep(&mut State, now: Instant)` implementing design D5's transition table: expired leases and nack-released messages at or past `max_deliveries` become `DEAD_LETTER`, or are deleted when `dead_letter_enabled` is false; everything else below the limit becomes visible `READY`
- [x] 2.2 Make `sweep` return `(returned_to_ready, moved_to_dead_letter)` counts so `ReapExpiredLeases` can report them, and confirm it is idempotent when nothing has expired
- [x] 2.3 Establish the D4 method shape for all eleven trait methods: lock, capture `now`, call `sweep`, delegate to a `&mut State` free function; add a module comment stating that no public method may call another
- [x] 2.4 Add `queue_mut(&mut State, &str) -> Result<&mut Queue>` returning `NOT_FOUND` / `QUEUE_NOT_FOUND`, and use it everywhere a `queue_id` is dereferenced

## 3. Queue registry

- [x] 3.1 Implement `ensure_queue`: create and report `created = true`, or return the stored config with `created = false` and never raise `QUEUE_CONFIG_CONFLICT` (design D9)
- [x] 3.2 Implement `get_queue_config` and `delete_queue`, with `purged_message_count` counting messages in all three states (design D9)
- [x] 3.3 Implement `list_queues`: order by `queue_id`, `next_page_token` is the last id returned and empty on the final page, `page_size = 0` returns all (design D9)
- [x] 3.4 Implement `get_queue_stats` counting each message under exactly one of ready/leased/dead-letter, read from the same locked state that serves leases
- [x] 3.5 Verify `r11_queue_isolation` passes

## 4. Enqueue and deduplication

- [x] 4.1 Implement `save_message`: store payload, attributes, priority; set `visible_at` from `visibility_delay` (absent means now); return `message_id` and `saved_at`
- [x] 4.2 Implement dedup lookup keyed by `idempotency_key` within the queue, returning the original `message_id` with `deduplicated = true` when the record is inside `dedup_window`
- [x] 4.3 Measure the dedup window from the original save, not the last attempt, and replace the record with a fresh one once the window has elapsed
- [x] 4.4 Confirm dedup records are never removed when their message is acked, deleted or dead-lettered
- [x] 4.5 Verify `r02_deduplication` and `r10_visibility_delay` pass

## 5. Delivery

- [x] 5.1 Implement eligibility: state is `Ready` and `visible_at <= now`
- [x] 5.2 Implement selection order `priority DESC, visible_at ASC, message_id ASC` by collecting eligible messages and sorting, per design D2
- [x] 5.3 Apply the delivery transition: increment `delivery_count`, issue a fresh `lease_token`, set `lease_expires_at = now + effective_ttl`, move to `Leased`
- [x] 5.4 Resolve the effective TTL: `lease_ttl` when present, otherwise the queue's `default_lease_ttl`; treat `batch_size = 0` as 1 and cap the returned count at `batch_size`
- [x] 5.5 Verify `r01_lifecycle` and `r07_priority` pass

## 6. Leases and settlement

- [x] 6.1 Implement the single stale-token predicate from design D9: `state != Leased || lease_token != supplied` yields `LEASE_LOST`
- [x] 6.2 Implement `renew_lease`: set `lease_expires_at = now + extension` (not current expiry plus extension), do not rotate the token, and rely on the sweep having already cleared expired leases so expiry is final
- [x] 6.3 Implement `settle_messages` as a per-item loop returning one `SettlementResult` per input in request order, never failing the RPC for an item-level problem
- [x] 6.4 Implement `OUTCOME_ACK_DELETE` (remove), `OUTCOME_RETRY` (back to `Ready` with `visible_at = now + retry_delay`, delay applied as given), and `OUTCOME_DEAD_LETTER` (forced, any `delivery_count`, retaining `dead_letter_reason`, subject to `dead_letter_enabled`)
- [x] 6.5 Return `MESSAGE_NOT_FOUND` per item for an unknown `message_id`, and confirm rejected items mutate nothing
- [x] 6.6 Verify `r03_lease_expiry`, `r04_stale_token`, `r05_retry_delay` and `r09_batch_settle` pass

## 7. Dead-lettering

- [x] 7.1 Route the nack path through the same limit check as expiry: a retry settlement on a message at `max_deliveries` dead-letters instead of releasing
- [x] 7.2 Confirm the expiry path dead-letters at the limit via `sweep` alone, with no second call site enforcing the limit
- [x] 7.3 Implement the `dead_letter_enabled = false` drop path on both routes and on forced dead-letter
- [x] 7.4 Confirm `DEAD_LETTER` is terminal: never eligible, always counted in `dead_letter_count`
- [x] 7.5 Verify `r06_dead_lettering` passes

## 8. Maintenance and identity

- [x] 8.1 Implement `reap_expired_leases`, honouring `grace_period` (zero means all expired) and reporting the counts from the sweep it performs
- [x] 8.2 Confirm expired leases are reclaimed with no call to `reap_expired_leases`, through `lease_messages` and through `get_queue_stats` alike
- [x] 8.3 Implement `get_provider_info` returning `provider_name = "memory"`, a non-empty `provider_version`, and `contract_version = kyu_conformance::spi::CONTRACT_VERSION`
- [x] 8.4 Verify `r12_reaping` and `r13_provider_info` pass

## 9. Validation

- [x] 9.1 Implement the collision-free §4 limits in `validate.rs`, all returning `INVALID_ARGUMENT` / `VALIDATION_FAILED`: payload 1 MiB, attributes 16 entries / 4 KiB, `idempotency_key` 256 B, `queue_id` non-empty and 256 B, `settlements` non-empty and 1000 items, `retry_delay` 12 h, `lease_ttl` and `extension` 12 h maximum, `batch_size` 100, `max_deliveries` at least 1
- [x] 9.2 Deliberately omit the 1 s minimum on `lease_ttl` and `extension`, with a comment at the check site naming design D8 and the four scenarios that lease with a 300 ms TTL
- [x] 9.3 Implement the §4 defaults as field semantics rather than validation: `lease_ttl: None`, `batch_size: 0`, `visibility_delay: None`, `retry_delay: ZERO`
- [x] 9.4 Re-run the full suite and confirm validation broke nothing

## 10. Concurrency and close-out

- [x] 10.1 Verify `r08_concurrency` passes, then run it repeatedly (at least 20 consecutive runs) to build confidence, since Rust has no race detector
- [x] 10.2 Audit for re-entrancy: confirm no public method calls another public method and no `&mut State` function acquires the lock
- [x] 10.3 Add unit tests in `crates/kyu-memory/` for the behaviours the oracle does not cover: `ensure_queue` config mismatch, `list_queues` paging, `delete_queue` counting dead-lettered messages, per-item `MESSAGE_NOT_FOUND`, empty settlement batch, and the validation limits from 9.1
- [x] 10.4 Run `cargo test -p kyu-memory --features conformance` and confirm all thirteen scenarios pass
- [x] 10.5 Run `cargo clippy --all-targets -- -D warnings` and `cargo fmt --all` clean
- [x] 10.6 Confirm `crates/kyu-conformance/` and `crates/kyu-memory/tests/conformance.rs` are untouched (`git diff --stat` shows no changes to either)
