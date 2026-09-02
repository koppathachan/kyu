//! Kyu v1 protocol specification §8.1 — provider conformance requirements,
//! in executable form.
//!
//! The numbering here tracks §8.1 one-to-one and deliberately so: a missing
//! requirement should be visible as a gap in the sequence.
//!
//! Each scenario takes a provider that has just been constructed. Scenarios use
//! disjoint queue ids, but they are written to be run against a fresh provider
//! each, which is what the generated wiring does.

use std::collections::{HashMap, HashSet};
use std::sync::Mutex;
use std::time::{Duration, Instant, SystemTime};

use crate::spi::*;

// ---------------------------------------------------------------------------
// Timing. Real durations against the provider's real clock (§3.8). Kept short
// so the suite stays fast, with wide margins so it stays deterministic.
// ---------------------------------------------------------------------------

/// A lease/delay short enough to expire inside a test.
const SHORT: Duration = Duration::from_millis(300);
/// A lease long enough that nothing expires mid-scenario.
const LONG: Duration = Duration::from_secs(30);
/// How long to keep polling for something that should become true.
const PATIENCE: Duration = Duration::from_secs(5);
/// Gap between polls.
const POLL: Duration = Duration::from_millis(20);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// A queue config with unremarkable defaults; scenarios override what they test.
fn cfg(queue_id: &str) -> StorageQueueConfig {
    StorageQueueConfig {
        queue_id: queue_id.to_string(),
        default_lease_ttl: LONG,
        max_deliveries: 5,
        retry_backoff_base: Duration::from_millis(10),
        retry_backoff_max: Duration::from_secs(1),
        dedup_window: Duration::from_secs(30),
        dead_letter_enabled: true,
    }
}

fn ensure<P: StorageProvider>(p: &P, config: StorageQueueConfig) {
    let id = config.queue_id.clone();
    let resp = p
        .ensure_queue(EnsureQueueRequest { config })
        .unwrap_or_else(|e| panic!("ensure_queue({id}) failed: {e}"));
    assert!(
        resp.created,
        "ensure_queue({id}): expected created=true for a new queue"
    );
}

fn save<P: StorageProvider>(p: &P, queue_id: &str, payload: &str) -> String {
    save_req(
        p,
        SaveMessageRequest {
            queue_id: queue_id.to_string(),
            payload: payload.as_bytes().to_vec(),
            ..Default::default()
        },
    )
    .message_id
}

fn save_req<P: StorageProvider>(p: &P, req: SaveMessageRequest) -> SaveMessageResponse {
    p.save_message(req).expect("save_message failed")
}

fn lease<P: StorageProvider>(p: &P, queue_id: &str, n: i32, ttl: Duration) -> Vec<LeasedMessage> {
    p.lease_messages(LeaseMessagesRequest {
        queue_id: queue_id.to_string(),
        batch_size: n,
        lease_ttl: Some(ttl),
    })
    .expect("lease_messages failed")
    .messages
}

fn lease_one<P: StorageProvider>(p: &P, queue_id: &str, ttl: Duration) -> Option<LeasedMessage> {
    lease(p, queue_id, 1, ttl).into_iter().next()
}

/// Poll until a message becomes leasable, or give up after [`PATIENCE`].
fn await_leasable<P: StorageProvider>(
    p: &P,
    queue_id: &str,
    ttl: Duration,
) -> Option<LeasedMessage> {
    let deadline = Instant::now() + PATIENCE;
    loop {
        if let Some(m) = lease_one(p, queue_id, ttl) {
            return Some(m);
        }
        if Instant::now() >= deadline {
            return None;
        }
        std::thread::sleep(POLL);
    }
}

fn settle<P: StorageProvider>(p: &P, items: Vec<StorageSettlement>) -> Vec<SettlementResult> {
    p.settle_messages(SettleMessagesRequest { settlements: items })
        .expect("settle_messages failed")
        .results
}

fn settlement(m: &LeasedMessage, outcome: Outcome) -> StorageSettlement {
    StorageSettlement {
        queue_id: m.queue_id.clone(),
        message_id: m.message_id.clone(),
        lease_token: m.lease_token.clone(),
        outcome,
        retry_delay: Duration::ZERO,
        dead_letter_reason: String::new(),
    }
}

fn ack<P: StorageProvider>(p: &P, m: &LeasedMessage) {
    let r = settle(p, vec![settlement(m, Outcome::AckDelete)]);
    assert_eq!(
        r.len(),
        1,
        "settle_messages must return one result per input item (§7)"
    );
    assert_eq!(
        r[0].code,
        code::OK,
        "ack of a live lease must succeed, got {:?}",
        r[0]
    );
}

fn nack<P: StorageProvider>(p: &P, m: &LeasedMessage, retry_delay: Duration) {
    let mut s = settlement(m, Outcome::Retry);
    s.retry_delay = retry_delay;
    let r = settle(p, vec![s]);
    assert_eq!(
        r[0].code,
        code::OK,
        "nack of a live lease must succeed, got {:?}",
        r[0]
    );
}

fn stats<P: StorageProvider>(p: &P, queue_id: &str) -> GetQueueStatsResponse {
    p.get_queue_stats(GetQueueStatsRequest {
        queue_id: queue_id.to_string(),
    })
    .expect("get_queue_stats failed")
}

fn assert_stats<P: StorageProvider>(p: &P, queue_id: &str, ready: i64, leased: i64, dead: i64) {
    let s = stats(p, queue_id);
    assert_eq!(
        s,
        GetQueueStatsResponse {
            ready_count: ready,
            leased_count: leased,
            dead_letter_count: dead
        },
        "queue {queue_id} statistics"
    );
}

fn assert_lease_lost(err: &Error, what: &str) {
    assert_eq!(
        (err.code, err.reason.as_str()),
        (code::FAILED_PRECONDITION, reason::LEASE_LOST),
        "{what} must fail FAILED_PRECONDITION / LEASE_LOST (§3.3.2, §7)"
    );
}

// ---------------------------------------------------------------------------
// §8.1.1 — Lifecycle
// ---------------------------------------------------------------------------

/// save → lease → ack. The message is not leasable while a live lease exists,
/// and is gone after ack.
pub fn r01_lifecycle<P: StorageProvider>(p: &P) {
    ensure(p, cfg("lifecycle"));
    let attributes = HashMap::from([("trace".to_string(), "abc".to_string())]);
    let id = save_req(
        p,
        SaveMessageRequest {
            queue_id: "lifecycle".into(),
            payload: b"hello".to_vec(),
            attributes: attributes.clone(),
            ..Default::default()
        },
    )
    .message_id;
    assert_stats(p, "lifecycle", 1, 0, 0);

    let m = lease_one(p, "lifecycle", LONG).expect("a saved message must be leasable");
    assert_eq!(m.message_id, id);
    assert_eq!(m.queue_id, "lifecycle");
    assert_eq!(m.payload, b"hello");
    assert_eq!(
        m.attributes, attributes,
        "attributes are returned on delivery (§6)"
    );
    assert_eq!(
        m.delivery_count, 1,
        "delivery_count includes the current delivery (§3.4)"
    );
    assert!(
        !m.lease_token.is_empty(),
        "every delivery issues a lease token (§3.3.1)"
    );
    assert!(
        m.lease_expires_at > SystemTime::now(),
        "lease_expires_at must be in the future"
    );
    assert_stats(p, "lifecycle", 0, 1, 0);

    assert!(
        lease_one(p, "lifecycle", LONG).is_none(),
        "a message under a live lease must not be returned to another consumer (§3.3.6)"
    );

    ack(p, &m);
    assert_stats(p, "lifecycle", 0, 0, 0);
    assert!(
        lease_one(p, "lifecycle", LONG).is_none(),
        "an acked message is gone"
    );
}

// ---------------------------------------------------------------------------
// §8.1.2 — Deduplication
// ---------------------------------------------------------------------------

/// The same `(queue_id, idempotency_key)` inside the window yields the same
/// `message_id`, `deduplicated = true`, and exactly one stored copy. After the
/// window, a new message. Also holds after the original was acked (§3.7).
pub fn r02_deduplication<P: StorageProvider>(p: &P) {
    let dup = |key: &str, queue: &str| SaveMessageRequest {
        queue_id: queue.to_string(),
        payload: b"body".to_vec(),
        idempotency_key: key.to_string(),
        ..Default::default()
    };

    // --- inside the window: same id, flagged, one copy ---
    ensure(p, cfg("dedup"));
    let first = save_req(p, dup("k1", "dedup"));
    assert!(
        !first.deduplicated,
        "the first save with a key is not a duplicate"
    );
    let second = save_req(p, dup("k1", "dedup"));
    assert_eq!(
        second.message_id, first.message_id,
        "a duplicate returns the ORIGINAL message_id"
    );
    assert!(
        second.deduplicated,
        "a duplicate is flagged deduplicated=true"
    );
    assert_stats(p, "dedup", 1, 0, 0);

    // A different key is a different message.
    let other = save_req(p, dup("k2", "dedup"));
    assert_ne!(other.message_id, first.message_id);
    assert_stats(p, "dedup", 2, 0, 0);

    // --- dedup is scoped to one queue (§3.7) ---
    ensure(p, cfg("dedup-other"));
    let elsewhere = save_req(p, dup("k1", "dedup-other"));
    assert_ne!(
        elsewhere.message_id, first.message_id,
        "the same key in two queues is two messages (§3.7)"
    );
    assert!(!elsewhere.deduplicated);

    // --- the record outlives the message (§3.7) ---
    let m = lease_one(p, "dedup-other", LONG).expect("leasable");
    ack(p, &m);
    let after_ack = save_req(p, dup("k1", "dedup-other"));
    assert_eq!(
        after_ack.message_id, elsewhere.message_id,
        "a duplicate inside the window returns the original id even after it was acked (§3.7)"
    );
    assert!(after_ack.deduplicated);
    assert_stats(p, "dedup-other", 0, 0, 0);

    // --- after the window: a new message ---
    let mut short_window = cfg("dedup-window");
    short_window.dedup_window = SHORT;
    ensure(p, short_window);
    let before = save_req(p, dup("k1", "dedup-window"));
    std::thread::sleep(SHORT + Duration::from_millis(250));
    let after = save_req(p, dup("k1", "dedup-window"));
    assert_ne!(
        after.message_id, before.message_id,
        "after the window the same key produces a new message (§3.7)"
    );
    assert!(!after.deduplicated);
}

// ---------------------------------------------------------------------------
// §8.1.3 — Lease expiry
// ---------------------------------------------------------------------------

/// Leasable again after TTL; `delivery_count` incremented on redelivery; the
/// previous token is dead.
pub fn r03_lease_expiry<P: StorageProvider>(p: &P) {
    ensure(p, cfg("expiry"));
    let id = save(p, "expiry", "x");

    let first = lease_one(p, "expiry", SHORT).expect("leasable");
    assert_eq!(first.delivery_count, 1);

    let second = await_leasable(p, "expiry", LONG)
        .expect("an expired lease must make the message leasable again (§3.3.5)");
    assert_eq!(second.message_id, id);
    assert_eq!(
        second.delivery_count, 2,
        "delivery_count is incremented on redelivery (§3.4)"
    );
    assert_ne!(
        second.lease_token, first.lease_token,
        "every delivery issues a fresh lease token (§3.3.1)"
    );

    let r = settle(p, vec![settlement(&first, Outcome::AckDelete)]);
    assert_eq!(
        (r[0].code, r[0].error_reason.as_str()),
        (code::FAILED_PRECONDITION, reason::LEASE_LOST),
        "the token from the previous delivery is dead (§3.3.2)"
    );

    ack(p, &second);
    assert_stats(p, "expiry", 0, 0, 0);
}

// ---------------------------------------------------------------------------
// §8.1.4 — Stale token
// ---------------------------------------------------------------------------

/// Renew and settle with a rotated *or expired* token fail with `LEASE_LOST`
/// and leave the message untouched.
pub fn r04_stale_token<P: StorageProvider>(p: &P) {
    ensure(p, cfg("stale"));
    save(p, "stale", "x");
    let m = lease_one(p, "stale", LONG).expect("leasable");

    // --- a token that was never issued ---
    let err = p
        .renew_lease(RenewLeaseRequest {
            queue_id: "stale".into(),
            message_id: m.message_id.clone(),
            lease_token: "not-a-real-token".into(),
            extension: LONG,
        })
        .expect_err("renew with a bogus token must fail");
    assert_lease_lost(&err, "renew_lease with an unissued token");

    let mut bogus = settlement(&m, Outcome::AckDelete);
    bogus.lease_token = "not-a-real-token".into();
    let r = settle(p, vec![bogus]);
    assert_eq!(
        (r[0].code, r[0].error_reason.as_str()),
        (code::FAILED_PRECONDITION, reason::LEASE_LOST),
        "settle with an unissued token must fail LEASE_LOST"
    );

    // Untouched: the real token still works.
    assert_stats(p, "stale", 0, 1, 0);
    let renewed = p
        .renew_lease(RenewLeaseRequest {
            queue_id: "stale".into(),
            message_id: m.message_id.clone(),
            lease_token: m.lease_token.clone(),
            extension: LONG,
        })
        .expect("renew with the live token must succeed after a rejected attempt");
    assert!(renewed.lease_expires_at > SystemTime::now());

    // --- an expired token (§3.3.4: expiry is final) ---
    ensure(p, cfg("stale-expired"));
    save(p, "stale-expired", "x");
    let short = lease_one(p, "stale-expired", SHORT).expect("leasable");
    std::thread::sleep(SHORT + Duration::from_millis(250));
    let err = p
        .renew_lease(RenewLeaseRequest {
            queue_id: "stale-expired".into(),
            message_id: short.message_id.clone(),
            lease_token: short.lease_token.clone(),
            extension: LONG,
        })
        .expect_err("renew after expiry must fail even if not yet redelivered (§3.3.4)");
    assert_lease_lost(&err, "renew_lease after expiry");
}

// ---------------------------------------------------------------------------
// §8.1.5 — Retry delay
// ---------------------------------------------------------------------------

/// A nack with a delay leaves the message invisible for at least that duration,
/// then leasable.
pub fn r05_retry_delay<P: StorageProvider>(p: &P) {
    ensure(p, cfg("retry"));
    let id = save(p, "retry", "x");
    let m = lease_one(p, "retry", LONG).expect("leasable");

    let released = Instant::now();
    nack(p, &m, SHORT);

    assert!(
        lease_one(p, "retry", LONG).is_none(),
        "a nack with a retry delay must leave the message invisible (§3.5)"
    );

    let again = await_leasable(p, "retry", LONG).expect("the message must return after the delay");
    assert!(
        released.elapsed() >= SHORT,
        "the message became visible after {:?}, before its {SHORT:?} retry delay (§3.5)",
        released.elapsed()
    );
    assert_eq!(again.message_id, id);
    assert_eq!(again.delivery_count, 2);
}

// ---------------------------------------------------------------------------
// §8.1.6 — Dead-lettering
// ---------------------------------------------------------------------------

/// A message reaching `max_deliveries` lands in `DEAD_LETTER`, is never
/// delivered again, and is counted in stats. With `dead_letter_enabled = false`
/// it is dropped instead. A forced dead-letter works below the limit.
pub fn r06_dead_lettering<P: StorageProvider>(p: &P) {
    // --- reaching the limit ---
    let mut limited = cfg("dlq");
    limited.max_deliveries = 2;
    ensure(p, limited);
    save(p, "dlq", "x");

    let d1 = lease_one(p, "dlq", LONG).expect("delivery 1");
    assert_eq!(d1.delivery_count, 1);
    nack(p, &d1, Duration::ZERO);

    let d2 = await_leasable(p, "dlq", LONG).expect("delivery 2");
    assert_eq!(d2.delivery_count, 2);
    nack(p, &d2, Duration::ZERO);

    assert!(
        await_leasable(p, "dlq", LONG).is_none(),
        "with max_deliveries=2 a consumer sees the message at most twice (§3.4)"
    );
    assert_stats(p, "dlq", 0, 0, 1);

    // --- the same limit reached via lease expiry rather than nack ---
    let mut by_expiry = cfg("dlq-expiry");
    by_expiry.max_deliveries = 1;
    ensure(p, by_expiry);
    save(p, "dlq-expiry", "x");
    let only = lease_one(p, "dlq-expiry", SHORT).expect("delivery 1");
    assert_eq!(only.delivery_count, 1);
    std::thread::sleep(SHORT + Duration::from_millis(250));
    assert!(
        await_leasable(p, "dlq-expiry", LONG).is_none(),
        "a message at max_deliveries must be dead-lettered on expiry, not redelivered (§3.4)"
    );
    assert_stats(p, "dlq-expiry", 0, 0, 1);

    // --- dead_letter_enabled = false: dropped instead (§3.6) ---
    let mut dropping = cfg("dlq-off");
    dropping.max_deliveries = 1;
    dropping.dead_letter_enabled = false;
    ensure(p, dropping);
    save(p, "dlq-off", "x");
    let d = lease_one(p, "dlq-off", LONG).expect("delivery 1");
    nack(p, &d, Duration::ZERO);
    assert!(
        await_leasable(p, "dlq-off", LONG).is_none(),
        "not redelivered past the limit"
    );
    assert_stats(p, "dlq-off", 0, 0, 0);

    // --- forced dead-letter below the limit (§3.6) ---
    ensure(p, cfg("dlq-forced"));
    save(p, "dlq-forced", "x");
    let f = lease_one(p, "dlq-forced", LONG).expect("leasable");
    assert_eq!(f.delivery_count, 1, "well below max_deliveries=5");
    let mut forced = settlement(&f, Outcome::DeadLetter);
    forced.dead_letter_reason = "poison".into();
    let r = settle(p, vec![forced]);
    assert_eq!(
        r[0].code,
        code::OK,
        "a forced dead-letter must succeed, got {:?}",
        r[0]
    );
    assert_stats(p, "dlq-forced", 0, 0, 1);
    assert!(
        await_leasable(p, "dlq-forced", LONG).is_none(),
        "a dead-lettered message is never delivered (§3.6)"
    );
}

// ---------------------------------------------------------------------------
// §8.1.7 — Priority
// ---------------------------------------------------------------------------

/// Among visible messages, higher priority is delivered first; ties broken per
/// §3.2 (`visible_at ASC, message_id ASC`, which for undelayed sequential saves
/// is enqueue order).
pub fn r07_priority<P: StorageProvider>(p: &P) {
    ensure(p, cfg("prio"));
    let save_at = |priority: i32, payload: &str| {
        save_req(
            p,
            SaveMessageRequest {
                queue_id: "prio".into(),
                payload: payload.as_bytes().to_vec(),
                priority,
                ..Default::default()
            },
        )
        .message_id
    };
    let a = save_at(0, "a");
    let b = save_at(5, "b");
    let c = save_at(-3, "c");
    let d = save_at(5, "d");

    let got = lease(p, "prio", 4, LONG);
    let order: Vec<String> = got.iter().map(|m| m.message_id.clone()).collect();
    assert_eq!(
        order,
        vec![b, d, a, c],
        "delivery order must be priority DESC, then visible_at ASC, then message_id ASC (§3.2)"
    );
}

// ---------------------------------------------------------------------------
// §8.1.8 — Concurrency
// ---------------------------------------------------------------------------

/// N >= 32 concurrent leasers against one queue of M >= 1000 messages, each
/// acked exactly once: zero double-delivery while leases are live, zero lost
/// messages, and the total delivered equals M.
pub fn r08_concurrency<P: StorageProvider>(p: &P) {
    const N: usize = 32;
    const M: usize = 1000;

    ensure(p, cfg("race"));
    let mut expected = HashSet::with_capacity(M);
    for i in 0..M {
        expected.insert(save(p, "race", &format!("m{i}")));
    }
    assert_stats(p, "race", M as i64, 0, 0);

    let seen: Mutex<Vec<String>> = Mutex::new(Vec::with_capacity(M));

    std::thread::scope(|scope| {
        for _ in 0..N {
            scope.spawn(|| {
                loop {
                    // A long TTL: nothing may expire and be legitimately
                    // redelivered while this test is running.
                    let batch = lease(p, "race", 10, LONG);
                    if batch.is_empty() {
                        // Empty only means *this* worker found nothing; another
                        // may still be holding a lease it is about to ack. The
                        // drain is over when the queue itself is empty.
                        let s = stats(p, "race");
                        if s.ready_count == 0 && s.leased_count == 0 {
                            return;
                        }
                        std::thread::sleep(POLL);
                        continue;
                    }
                    let ids: Vec<String> = batch.iter().map(|m| m.message_id.clone()).collect();
                    let results = settle(
                        p,
                        batch
                            .iter()
                            .map(|m| settlement(m, Outcome::AckDelete))
                            .collect(),
                    );
                    for (r, id) in results.iter().zip(&ids) {
                        assert_eq!(
                            r.code,
                            code::OK,
                            "acking a message under a live lease must succeed (message {id}): {r:?}"
                        );
                    }
                    seen.lock().unwrap().extend(ids);
                }
            });
        }
    });

    let seen = seen.into_inner().unwrap();
    let unique: HashSet<&String> = seen.iter().collect();

    assert_eq!(
        unique.len(),
        seen.len(),
        "double delivery: {} of {} deliveries were duplicates — no message may be leased twice \
         while a lease is live (§8.1.8)",
        seen.len() - unique.len(),
        seen.len()
    );
    assert_eq!(seen.len(), M, "total delivered must equal M = {M}");
    let missing: Vec<&String> = expected.iter().filter(|id| !unique.contains(id)).collect();
    assert!(
        missing.is_empty(),
        "{} messages were never delivered",
        missing.len()
    );
    assert_stats(p, "race", 0, 0, 0);
}

// ---------------------------------------------------------------------------
// §8.1.9 — Batch settle
// ---------------------------------------------------------------------------

/// A batch mixing valid and stale items returns per-item results; valid items
/// are applied; invalid ones change nothing.
pub fn r09_batch_settle<P: StorageProvider>(p: &P) {
    ensure(p, cfg("batch"));
    save(p, "batch", "one");
    save(p, "batch", "two");
    let leased = lease(p, "batch", 2, LONG);
    assert_eq!(leased.len(), 2);
    let (good, stale) = (&leased[0], &leased[1]);

    let mut bad = settlement(stale, Outcome::AckDelete);
    bad.lease_token = "rotated-away".into();

    let results = settle(p, vec![settlement(good, Outcome::AckDelete), bad]);

    assert_eq!(
        results.len(),
        2,
        "one result per input item, in request order (§7)"
    );
    assert_eq!(results[0].message_id, good.message_id);
    assert_eq!(
        results[0].code,
        code::OK,
        "the valid item is applied: {:?}",
        results[0]
    );
    assert_eq!(results[1].message_id, stale.message_id);
    assert_eq!(
        (results[1].code, results[1].error_reason.as_str()),
        (code::FAILED_PRECONDITION, reason::LEASE_LOST),
        "the stale item fails on its own without failing the batch (§7)"
    );

    // The invalid item changed nothing: its real lease is still live and settles.
    assert_stats(p, "batch", 0, 1, 0);
    ack(p, stale);
    assert_stats(p, "batch", 0, 0, 0);
}

// ---------------------------------------------------------------------------
// §8.1.10 — Visibility delay
// ---------------------------------------------------------------------------

/// An initial delay on save is honoured.
pub fn r10_visibility_delay<P: StorageProvider>(p: &P) {
    ensure(p, cfg("delayed"));
    let saved = Instant::now();
    let id = save_req(
        p,
        SaveMessageRequest {
            queue_id: "delayed".into(),
            payload: b"later".to_vec(),
            visibility_delay: Some(SHORT),
            ..Default::default()
        },
    )
    .message_id;

    assert!(
        lease_one(p, "delayed", LONG).is_none(),
        "a message saved with a visibility delay must not be immediately leasable (§3.2)"
    );

    let m = await_leasable(p, "delayed", LONG).expect("the message must appear after its delay");
    assert!(
        saved.elapsed() >= SHORT,
        "the message became visible after {:?}, before its {SHORT:?} delay",
        saved.elapsed()
    );
    assert_eq!(m.message_id, id);
    assert_eq!(m.delivery_count, 1, "a delay is not a delivery");
}

// ---------------------------------------------------------------------------
// §8.1.11 — Queue isolation
// ---------------------------------------------------------------------------

/// Messages never cross queues; deleting a queue purges its messages and
/// reports the count.
pub fn r11_queue_isolation<P: StorageProvider>(p: &P) {
    ensure(p, cfg("left"));
    ensure(p, cfg("right"));
    save(p, "left", "l1");
    save(p, "left", "l2");
    save(p, "right", "r1");

    let from_right = lease(p, "right", 10, LONG);
    assert_eq!(
        from_right.len(),
        1,
        "a lease must only ever see its own queue"
    );
    assert_eq!(from_right[0].queue_id, "right");
    assert_eq!(from_right[0].payload, b"r1");

    let deleted = p
        .delete_queue(DeleteQueueRequest {
            queue_id: "left".into(),
        })
        .expect("delete_queue failed");
    assert_eq!(
        deleted.purged_message_count, 2,
        "delete_queue reports how many messages it purged"
    );

    let err = p
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "left".into(),
        })
        .expect_err("a deleted queue is gone");
    assert_eq!(
        (err.code, err.reason.as_str()),
        (code::NOT_FOUND, reason::QUEUE_NOT_FOUND),
        "an unknown queue is NOT_FOUND / QUEUE_NOT_FOUND (§7)"
    );

    assert_stats(p, "right", 0, 1, 0);
}

// ---------------------------------------------------------------------------
// §8.1.12 — Reaping
// ---------------------------------------------------------------------------

/// Expired leases are reclaimed *without* any call to `ReapExpiredLeases`; when
/// the RPC is called, its counts are consistent with induced expirations.
pub fn r12_reaping<P: StorageProvider>(p: &P) {
    // --- reclaimed with no administrative call at all (§3.3.7) ---
    ensure(p, cfg("reap-lazy"));
    save(p, "reap-lazy", "x");
    let m = lease_one(p, "reap-lazy", SHORT).expect("leasable");
    assert!(
        await_leasable(p, "reap-lazy", LONG).is_some(),
        "a provider must reclaim expired leases on its own; the engine never calls \
         ReapExpiredLeases for correctness (§3.3.7)"
    );
    drop(m);

    // --- the administrative trigger reports consistent counts ---
    ensure(p, cfg("reap-rpc"));
    const K: i64 = 3;
    for i in 0..K {
        save(p, "reap-rpc", &format!("m{i}"));
    }
    let leased = lease(p, "reap-rpc", K as i32, SHORT);
    assert_eq!(leased.len(), K as usize);
    std::thread::sleep(SHORT + Duration::from_millis(250));

    let reaped = p
        .reap_expired_leases(ReapExpiredLeasesRequest {
            grace_period: Duration::ZERO,
        })
        .expect("reap_expired_leases failed");
    assert!(
        reaped.returned_to_ready <= K,
        "reap reported {} messages returned to ready, but only {K} leases expired",
        reaped.returned_to_ready
    );
    assert_eq!(
        reaped.moved_to_dead_letter, 0,
        "max_deliveries is 5 and each message was delivered once; none is due for dead-lettering"
    );
    // However the provider got there — lazily or through the RPC — the outcome
    // is the same: the messages are READY again.
    assert_stats(p, "reap-rpc", K, 0, 0);

    let again = p
        .reap_expired_leases(ReapExpiredLeasesRequest {
            grace_period: Duration::ZERO,
        })
        .expect("reap_expired_leases failed");
    assert_eq!(
        (again.returned_to_ready, again.moved_to_dead_letter),
        (0, 0),
        "with nothing expired, a reap reclaims nothing"
    );
}

// ---------------------------------------------------------------------------
// §8.1.13 — Provider info
// ---------------------------------------------------------------------------

/// `GetProviderInfo` returns `contract_version = "kyu.storage.v1"`.
pub fn r13_provider_info<P: StorageProvider>(p: &P) {
    let info = p
        .get_provider_info(GetProviderInfoRequest {})
        .expect("get_provider_info failed");
    assert_eq!(
        info.contract_version, CONTRACT_VERSION,
        "contract_version MUST be {CONTRACT_VERSION} (§6)"
    );
    assert!(
        !info.provider_name.is_empty(),
        "a provider names itself (§6)"
    );
    assert!(
        !info.provider_version.is_empty(),
        "a provider reports its version (§6)"
    );
}
