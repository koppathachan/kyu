## Context

`crates/kyu-memory/src/lib.rs` is a single doc comment. The contract already exists and is fixed:

- `kyu_conformance::spi` — §6's proto transcribed method-for-method into a native trait.
- `kyu_conformance::scenarios` — §8.1's thirteen requirements as executable checks (826 lines).
- `crates/kyu-memory/tests/conformance.rs` — the wiring, gated behind the `conformance` cargo feature.

Two properties of the trait settle more design than the prose does:

```rust
pub trait StorageProvider: Send + Sync {
    fn lease_messages(&self, req: LeaseMessagesRequest) -> Result<LeaseMessagesResponse>;
    // ... every method takes &self
}
```

`&self` on every mutating method forces interior mutability. `Send + Sync` means the engine shares one provider across concurrent callers. So the concurrency question (§8.1.8) is not *how do we coordinate* but *what do we lock*.

The other shaping constraint is that the oracle is fixed and cannot be edited. Where the spec's prose and the oracle disagree, the oracle is the definition of done.

## Goals / Non-Goals

**Goals:**

- All thirteen §8.1 scenarios pass under `cargo test -p kyu-memory --features conformance`.
- A correctness argument that fits in a sentence, not a case analysis — the implementation should be readable as a reference for the SPI's semantics.
- Zero dependencies beyond `std`. No async runtime, no background threads.
- Every behaviour the SPI defines but the oracle does not exercise is decided deliberately and recorded here, not left to whichever branch the compiler took first.

**Non-Goals:**

- Performance beyond what §8.1.8 demands (32 leasers, 1000 messages). No sharding, no lock-free structures, no index tuning.
- Durability. This is an in-memory provider; process exit is data loss, by design.
- The queue engine (§5, §8.2), any non-memory provider, the gRPC binding, and every §9 non-goal.
- Retry/backoff *policy*. §3.5 puts it in the engine; the provider applies the `retry_delay` it is handed.

## Decisions

### D1 — One global `Mutex<State>`, not per-queue locks

```rust
pub struct MemoryProvider { state: Mutex<State> }

struct State {
    queues:  HashMap<String, Queue>,
    next_id: u64,
}

struct Queue {
    config:   StorageQueueConfig,
    messages: HashMap<String, Message>,          // single source of truth
    dedup:    HashMap<String, DedupRecord>,      // outlives the messages it names
}
```

Chosen over `RwLock<HashMap<QueueId, Arc<Mutex<Queue>>>>`. The argument is correctness, not simplicity:

- **`ReapExpiredLeasesRequest` carries no `queue_id`.** It is inherently a global operation. Per-queue locks would make its `returned_to_ready` / `moved_to_dead_letter` counts a smear across time; §8.1.12 wants them consistent with a single set of induced expirations.
- **`DeleteQueue` races.** With `Arc<Mutex<Queue>>`, a caller holding an `Arc` obtained before the delete can keep leasing from a queue that no longer exists. A global lock makes that unrepresentable.
- **§8.1.8 hammers one queue.** Per-queue locking buys exactly zero on the only scenario that measures concurrency.
- **`ListQueues` and `GetQueueStats`** get atomic snapshots for free.

Cost accepted: all provider traffic serialises. At the scale §8.1.8 defines (~100 lease calls over 1000 messages) this is sub-millisecond in total.

Lock acquisition uses `.unwrap_or_else(PoisonError::into_inner)` rather than `.unwrap()`. §8.1.8 asserts inside 32 spawned threads; a genuine failure there poisons the mutex, and 31 subsequent poison-panics would bury the assertion message that actually matters.

### D2 — Flat `HashMap` plus sort-per-lease, not sorted indexes

Delivery order is `priority DESC, visible_at ASC, message_id ASC` (§3.2) while *eligibility* is `visible_at <= now`. These are different predicates, which rules out the obvious structure:

- **`BinaryHeap` is unusable.** Its head may be a high-priority message whose `visible_at` is in the future, and a heap cannot skip past its own head cheaply.
- **A `BTreeSet` index** keyed `(Reverse(priority), visible_at, message_id)` would work — iterate in order, skip the not-yet-visible, take `batch_size`.
- **Chosen: neither.** Collect eligible messages from the queue's `HashMap`, sort, take `batch_size`.

Rationale: the `HashMap` plus an explicit `state` field is a single source of truth. Indexes add three structures that can drift out of sync with it, trading a cheap cost for a whole class of invariant bug. At §8.1.8's scale the sort is ~100 calls × ~500 messages — irrelevant.

Recorded as the upgrade path, should the provider ever be used in earnest: three time-keyed sets, where every lazy transition becomes a range query.

```
delayed : BTreeSet<(visible_at, id)>                 range(..=now) -> becomes visible
leases  : BTreeSet<(expires_at, id)>                 range(..=now) -> expires
ready   : BTreeSet<(Reverse(prio), visible_at, id)>  pop front     -> deliver
```

### D3 — Sweep at the top of every method, at one chokepoint

Three candidate strategies for reclaiming expired leases:

| | Strategy | Verdict |
|---|---|---|
| A | `sweep()` as the first act of every public method | **Chosen** |
| B | Sweep only in `lease_messages`, `get_queue_stats`, `reap_expired_leases` | Sufficient, but the sufficiency proof is a case analysis |
| C | Derive state at read time; never mutate on expiry | Rejected — see below |

The oracle constrains where sweeping must happen more tightly than §3.3.7 does:

| Scenario | Last call before the assertion | Sweep must run in |
|---|---|---|
| §8.1.12 `reap-lazy` | `lease_messages`, polled | `lease_messages` |
| §8.1.6 `dlq-expiry` | `get_queue_stats` after a failed lease | `lease_messages` or `get_queue_stats` |
| §8.1.12 `reap-rpc` | `reap_expired_leases`, then `get_queue_stats` — no lease between | `reap_expired_leases` and/or `get_queue_stats` |

**C is rejected** because `dead_letter_enabled = false` requires a *deletion* (§3.6), and a deletion cannot be derived. Pure derivation would need a tombstone that leaks for the process lifetime and corrupts `DeleteQueue`'s purge count.

**A over B** because it reduces the correctness argument to one sentence: *no method ever observes stale state.* For a reference implementation that is worth more than the microseconds B saves.

No background thread is required by anything in §8.1. §3.3.7 permits either; lazy reclamation is simpler and has no shutdown story to get wrong.

### D4 — `&self` methods lock and delegate; `&mut State` functions never lock

`std::sync::Mutex` is not reentrant. D3 puts a `sweep` call in every method, which makes accidental re-entry a live hazard — `sweep` reaching for `self.get_queue_stats()` would deadlock rather than fail loudly.

```rust
impl StorageProvider for MemoryProvider {
    fn lease_messages(&self, req: LeaseMessagesRequest) -> Result<LeaseMessagesResponse> {
        let mut st = self.lock();
        sweep(&mut st, Instant::now());
        do_lease(&mut st, req)
    }
}
```

**Rule: no public method calls another public method.** All logic lives in free functions over `&mut State`.

### D5 — `max_deliveries` is enforced in exactly one place

§3.4 and §3.6 describe two routes to `DEAD_LETTER` — a nack at the limit, and a lease expiry at the limit. They are the same rule applied at the same moment: *when a message would become eligible again*.

```
            save(visibility_delay)
                     |
                     v
     +--------------------------------+
     | READY        visible_at = T    | <-------------+
     +--------------------------------+               |
         | lease, iff now >= visible_at                | nack(retry_delay)
         | delivery_count += 1                         | visible_at = now + d
         | lease_token = fresh                         |
         v                                             |
     +--------------------------------+                |
     | LEASED  token, lease_expires_at | ---------------+
     +--------------------------------+
         |            |                      |
    ack  |     expiry |                      | forced DEAD_LETTER
 (delete)|            |                      | (any delivery_count)
         v            v                      v
      (gone)   delivery_count <  max  -> READY, visible now
               delivery_count >= max  -> DEAD_LETTER
                                         or DELETE if !dead_letter_enabled
```

`sweep` applies the bottom half. Both §8.1.6 paths fall out of it without special-casing, and §3.4's "MUST NOT be delivered again" becomes a structural property rather than a check that could be forgotten at one call site.

### D6 — Message ids are a zero-padded monotonic counter

§2 says ids SHOULD be lexicographically ordered by creation time and RECOMMENDS UUIDv7. What §3.2 actually needs is the ordering property, as the final tiebreak after `priority` and `visible_at`. §8.1.7 depends on it: two messages saved at priority 5 in the same clock tick must come back in save order.

`format!("{:020}", state.next_id)` gives lexicographic-equals-creation order, uniqueness, and the ≤128-character bound, with no dependency. UUIDv7 is a recommendation, not a requirement; the property it exists to provide is satisfied.

### D7 — One clock source: `Instant` internally, `SystemTime` only on the wire

The SPI types force `SystemTime` on responses (`lease_expires_at`, `saved_at`). `SystemTime` is not monotonic, and §8.1.5 / §8.1.10 assert elapsed time using `Instant` on the test side.

Store `Instant` for `visible_at`, `lease_expires_at` and dedup record creation — every comparison the provider makes. Convert to `SystemTime` only when filling a response field. §3.8 makes the provider's clock authoritative, so the conversion is a presentation concern.

### D8 — §4 limits: enforce, with one documented exception

§4 and the fixed oracle are in direct contradiction:

```
§4:         lease_ttl, RenewLease.extension  --  min 1 s, max 12 h
scenarios:  const SHORT: Duration = Duration::from_millis(300);
              lease_one(p, "expiry",        SHORT)   <- §8.1.3
              lease_one(p, "stale-expired", SHORT)   <- §8.1.4
              lease_one(p, "dlq-expiry",    SHORT)   <- §8.1.6
              lease(p,     "reap-rpc",   3, SHORT)   <- §8.1.12
```

Enforcing the minimum returns `VALIDATION_FAILED`, the suite's `.expect("lease_messages failed")` panics, and four scenarios fail.

Resolution: **§4's table is written in §5's client-API vocabulary** — it names `Dequeue.batch_size` and `Settle.settlements`, which are engine-side types. It binds a provider only by analogy, and where that analogy contradicts §8.1 the oracle wins.

| Bucket | Contents | Action |
|---|---|---|
| Defaults (§4 right column) | `lease_ttl: None` ⇒ queue `default_lease_ttl`; `batch_size: 0` ⇒ 1; `visibility_delay: None` ⇒ visible now; `retry_delay: ZERO` ⇒ immediate | **Implement.** These are field semantics, not validation. |
| Limits, collision-free | `payload` ≤ 1 MiB; `attributes` ≤ 16 entries / 4 KiB; `idempotency_key` ≤ 256 B; `queue_id` non-empty ≤ 256 B; `settlements` non-empty ≤ 1000; `retry_delay` ≤ 12 h; `max_deliveries` ≥ 1; `batch_size` ≤ 100; `extension` ≤ 12 h | **Implement**, `INVALID_ARGUMENT` / `VALIDATION_FAILED`. |
| Limits, collided | `lease_ttl` / `extension` **minimum** of 1 s | **Do not enforce.** Comment at the check site naming the four scenarios, so the omission reads as a decision. |

The 12 h *maximum* on `lease_ttl` is kept; only the minimum is dropped.

### D9 — Behaviours the SPI defines and the oracle does not pin

Each of these has more than one defensible answer and no test to choose between them. Deciding here rather than at the keyboard:

- **`EnsureQueue` on a config mismatch** — return `created = false` with the **stored** config. Never error. §6's own comment is explicit that the engine compares and decides; §7's `QUEUE_CONFIG_CONFLICT` is the engine's to raise, not the provider's.
- **`DeleteQueue.purged_message_count`** — all messages in all states, `DEAD_LETTER` included. §8.1.11 only exercises `READY`, but "purges its messages and reports the count" reads as all of them.
- **`dead_letter_reason`** — store it on the message. No v1 API returns it (redrive is deferred, §9), but discarding data the caller supplied is worse than holding it.
- **`ListQueues`** — order by `queue_id`; `next_page_token` is the last id returned; `page_size: 0` means all. Stable paging with no server-side cursor state.
- **Unknown queue, anywhere** — `NOT_FOUND` / `QUEUE_NOT_FOUND`, including on `SaveMessage` and `GetQueueConfig`.
- **Settling a message that does not exist** — per-item `NOT_FOUND` / `MESSAGE_NOT_FOUND`, never an RPC-level failure (§7 batch rule).
- **Stale-token rule, as one predicate** — `state != Leased || token != current_token` ⇒ `LEASE_LOST`. Because D3 sweeps first, an expired lease has already had its token cleared, so §3.3.4's "expiry is final, whether or not it has been redelivered" needs no separate branch.
- **`RenewLease`** — sets `lease_expires_at = now + extension` (§3.3.3, not current-expiry-plus-extension) and does **not** rotate the token.

## Risks / Trade-offs

- **Global mutex serialises all traffic** → Accepted. §8.1.8's scale makes it irrelevant, and D1's correctness arguments outweigh throughput for a memory provider. D2 records the index-based upgrade path if that ever changes.
- **Sweep-everywhere is O(messages) per call** → Accepted at this scale. Every call under D2's flat map already walks the queue; the sweep folds into that walk rather than adding a second one.
- **Deadlock if a public method calls another** → Mitigated structurally by D4: public methods only lock and delegate; all logic is `&mut State` free functions. A public method that takes no lock cannot be called from inside one.
- **Not enforcing §4's `lease_ttl` minimum leaves a documented MUST unmet** → Mitigated by recording the collision at the check site and in this document. The alternative is failing four conformance scenarios, and the oracle is fixed by rule.
- **Zero-padded counter ids leak insertion count** → Accepted. §2 requires only opacity to clients and ≤128 characters; the SPI has no adversarial party (§9 defers multi-tenancy and authz entirely).
- **`SystemTime` conversions could drift from the stored `Instant`** → Mitigated by converting once, at response-construction time, from a single `now` captured at method entry, rather than recomputing per field.
- **§8.1.8 failures are non-deterministic and Rust has no `-race`** → Mitigated by D1: with one mutex covering lease selection, `delivery_count`, token issue and settlement, the interleavings that produce double-delivery are unrepresentable rather than merely unlikely. Verified by running the scenario repeatedly.
- **`GetQueueStats` must be exact under the same lock as lease and settle** → §8.1.8's workers exit when they observe `ready_count == 0 && leased_count == 0`. Under-reporting `leased_count` for even an instant sends all 32 threads home early, and the failure surfaces as "messages were never delivered" rather than as a statistics bug. D1 makes stats a read of the same locked state.
