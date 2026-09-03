//! In-memory Kyu v1 storage provider.
//!
//! [`MemoryProvider`] implements [`kyu_conformance::StorageProvider`], the
//! storage provider SPI of protocol specification §6, against a single
//! `Mutex<State>`. It is durable for the lifetime of the process and no longer:
//! process exit is data loss, by design.
//!
//! # Shape
//!
//! Two rules hold everywhere in this crate, and between them they are the whole
//! correctness argument:
//!
//! 1. **Every public method sweeps first** (design D3), so no method ever
//!    observes a lease that has already expired.
//! 2. **No public method calls another public method** (design D4).
//!    `std::sync::Mutex` is not reentrant, so a public method reaching for
//!    another would deadlock rather than fail loudly. Public methods only lock,
//!    capture the clock, sweep, and delegate; all logic lives in free functions
//!    over `&mut State`, which never take the lock themselves.

pub mod state;
pub mod sweep;
pub mod validate;

use std::sync::{Mutex, MutexGuard, PoisonError};
use std::time::Instant;

use kyu_conformance::spi::*;

use crate::state::{
    DedupRecord, Message, MsgState, Now, Queue, State, next_lease_token, next_message_id, queue_mut,
};
use crate::sweep::{Released, dead_letter, release, sweep};

/// An in-memory [`StorageProvider`].
///
/// One global mutex rather than per-queue locks (design D1):
/// `ReapExpiredLeases` carries no `queue_id` and is inherently global,
/// `DeleteQueue` racing a lease is unrepresentable under one lock, and
/// `ListQueues` and `GetQueueStats` get atomic snapshots for free.
#[derive(Default)]
pub struct MemoryProvider {
    state: Mutex<State>,
}

impl MemoryProvider {
    pub fn new() -> Self {
        Self::default()
    }

    /// A poisoned mutex is recovered rather than propagated: the conformance
    /// suite asserts inside 32 spawned threads, and 31 poison-panics would bury
    /// the assertion message that actually matters (design D1).
    fn lock(&self) -> MutexGuard<'_, State> {
        self.state.lock().unwrap_or_else(PoisonError::into_inner)
    }
}

// ---------------------------------------------------------------------------
// Queue registry
// ---------------------------------------------------------------------------

/// Design D9: a config mismatch is never an error here. §6's own comment says
/// the response carries the *stored* config; comparing it against the requested
/// one, and raising `QUEUE_CONFIG_CONFLICT`, is the engine's job (§8.2).
fn do_ensure_queue(st: &mut State, req: EnsureQueueRequest) -> Result<EnsureQueueResponse> {
    let queue_id = req.config.queue_id.clone();
    if let Some(existing) = st.queues.get(&queue_id) {
        return Ok(EnsureQueueResponse {
            config: existing.config.clone(),
            created: false,
        });
    }
    let config = req.config.clone();
    st.queues.insert(queue_id, Queue::new(req.config));
    Ok(EnsureQueueResponse {
        config,
        created: true,
    })
}

fn do_get_queue_config(
    st: &mut State,
    req: GetQueueConfigRequest,
) -> Result<GetQueueConfigResponse> {
    let queue = queue_mut(st, &req.queue_id)?;
    Ok(GetQueueConfigResponse {
        config: queue.config.clone(),
    })
}

/// Ordered by `queue_id`; `page_token` is the last id of the previous page, so
/// paging needs no server-side cursor state (design D9).
fn do_list_queues(st: &mut State, req: ListQueuesRequest) -> Result<ListQueuesResponse> {
    let mut ids: Vec<&String> = st
        .queues
        .keys()
        .filter(|id| req.page_token.is_empty() || **id > req.page_token)
        .collect();
    ids.sort();

    let page_size = if req.page_size > 0 {
        req.page_size as usize
    } else {
        ids.len()
    };
    let has_more = ids.len() > page_size;
    ids.truncate(page_size);

    let next_page_token = match ids.last() {
        Some(last) if has_more => (*last).clone(),
        _ => String::new(),
    };
    let queues = ids
        .into_iter()
        .map(|id| st.queues[id].config.clone())
        .collect();

    Ok(ListQueuesResponse {
        queues,
        next_page_token,
    })
}

/// `purged_message_count` covers messages in all three states, dead-lettered
/// ones included (design D9).
fn do_delete_queue(st: &mut State, req: DeleteQueueRequest) -> Result<DeleteQueueResponse> {
    let queue = st
        .queues
        .remove(&req.queue_id)
        .ok_or_else(Error::queue_not_found)?;
    Ok(DeleteQueueResponse {
        purged_message_count: queue.messages.len() as i64,
    })
}

/// Every stored message falls under exactly one of the three counts, read from
/// the same locked state that serves leases and settlements. §8.1.8's workers
/// go home when they see `ready_count == 0 && leased_count == 0`, so
/// under-reporting `leased_count` for even an instant would strand messages.
fn do_get_queue_stats(st: &mut State, req: GetQueueStatsRequest) -> Result<GetQueueStatsResponse> {
    let queue = queue_mut(st, &req.queue_id)?;
    let mut resp = GetQueueStatsResponse {
        ready_count: 0,
        leased_count: 0,
        dead_letter_count: 0,
    };
    for msg in queue.messages.values() {
        match msg.state {
            MsgState::Ready => resp.ready_count += 1,
            MsgState::Leased => resp.leased_count += 1,
            MsgState::DeadLetter => resp.dead_letter_count += 1,
        }
    }
    Ok(resp)
}

// ---------------------------------------------------------------------------
// Enqueue
// ---------------------------------------------------------------------------

fn do_save_message(
    st: &mut State,
    now: Now,
    req: SaveMessageRequest,
) -> Result<SaveMessageResponse> {
    // A live dedup record short-circuits the save, without consuming an id.
    let queue = queue_mut(st, &req.queue_id)?;
    if !req.idempotency_key.is_empty()
        && let Some(record) = queue.dedup.get(&req.idempotency_key)
        && now.instant.saturating_duration_since(record.created_at) < queue.config.dedup_window
    {
        // `saved_at` describes the save this response names, which is the
        // original one — reporting the duplicate attempt's clock would
        // describe a save that never happened.
        return Ok(SaveMessageResponse {
            message_id: record.message_id.clone(),
            saved_at: now.to_system(record.created_at),
            deduplicated: true,
        });
    }

    let message_id = next_message_id(st);
    let queue = queue_mut(st, &req.queue_id)?;

    // §4 default: an absent `visibility_delay` means visible now.
    let visible_at = now.instant + req.visibility_delay.unwrap_or_default();
    queue.messages.insert(
        message_id.clone(),
        Message {
            id: message_id.clone(),
            payload: req.payload,
            priority: req.priority,
            attributes: req.attributes,
            state: MsgState::Ready,
            visible_at,
            delivery_count: 0,
            lease_token: None,
            lease_expires_at: None,
            dead_letter_reason: String::new(),
        },
    );

    // The record is stamped with *this* save, so the window is measured from
    // the original and not from the latest attempt (§3.7). It is never removed
    // when the message it names is acked, deleted or dead-lettered — only
    // replaced once its window has elapsed, and dropped with its queue.
    if !req.idempotency_key.is_empty() {
        queue.dedup.insert(
            req.idempotency_key,
            DedupRecord {
                message_id: message_id.clone(),
                created_at: now.instant,
            },
        );
    }

    Ok(SaveMessageResponse {
        message_id,
        saved_at: now.sys,
        deduplicated: false,
    })
}

// ---------------------------------------------------------------------------
// Delivery
// ---------------------------------------------------------------------------

/// Eligible messages are collected and sorted per delivery rather than kept in
/// an index (design D2): the `HashMap` stays the single source of truth, with
/// no secondary structure that can drift out of sync with it.
fn do_lease_messages(
    st: &mut State,
    now: Now,
    req: LeaseMessagesRequest,
) -> Result<LeaseMessagesResponse> {
    let queue = queue_mut(st, &req.queue_id)?;

    // §4 defaults: an absent `lease_ttl` means the queue's default, and a
    // `batch_size` of zero means one.
    let ttl = req.lease_ttl.unwrap_or(queue.config.default_lease_ttl);
    let batch_size = if req.batch_size == 0 {
        1
    } else {
        req.batch_size as usize
    };

    // §3.2: priority DESC, visible_at ASC, message_id ASC.
    let mut eligible: Vec<(i32, Instant, &String)> = queue
        .messages
        .values()
        .filter(|m| m.state == MsgState::Ready && m.visible_at <= now.instant)
        .map(|m| (m.priority, m.visible_at, &m.id))
        .collect();
    eligible.sort_by(|a, b| b.0.cmp(&a.0).then(a.1.cmp(&b.1)).then(a.2.cmp(b.2)));
    eligible.truncate(batch_size);
    let ids: Vec<String> = eligible.into_iter().map(|(_, _, id)| id.clone()).collect();

    let tokens: Vec<String> = ids.iter().map(|_| next_lease_token(st)).collect();

    let queue = queue_mut(st, &req.queue_id)?;
    let lease_expires_at = now.instant + ttl;
    let mut messages = Vec::with_capacity(ids.len());
    for (id, token) in ids.iter().zip(tokens) {
        let Some(msg) = queue.messages.get_mut(id) else {
            continue;
        };
        msg.state = MsgState::Leased;
        msg.delivery_count += 1;
        msg.lease_token = Some(token.clone());
        msg.lease_expires_at = Some(lease_expires_at);

        messages.push(LeasedMessage {
            message_id: msg.id.clone(),
            queue_id: req.queue_id.clone(),
            payload: msg.payload.clone(),
            priority: msg.priority,
            delivery_count: msg.delivery_count,
            lease_token: token,
            lease_expires_at: now.to_system(lease_expires_at),
            attributes: msg.attributes.clone(),
        });
    }

    Ok(LeaseMessagesResponse { messages })
}

// ---------------------------------------------------------------------------
// Leases and settlement
// ---------------------------------------------------------------------------

/// The whole stale-token rule, as one predicate (design D9): anything but the
/// token of the current delivery is `LEASE_LOST`. Because the sweep has already
/// run, an expired lease has had its token cleared, so §3.3.4's "expiry is
/// final, whether or not it has been redelivered" needs no separate branch.
fn lease_is_live(msg: &Message, token: &str) -> bool {
    msg.state == MsgState::Leased && msg.lease_token.as_deref() == Some(token)
}

/// §3.3.3: the new expiry is `now + extension`, not the current expiry plus
/// `extension`, and the token is not rotated.
fn do_renew_lease(st: &mut State, now: Now, req: RenewLeaseRequest) -> Result<RenewLeaseResponse> {
    let queue = queue_mut(st, &req.queue_id)?;
    let msg = queue
        .messages
        .get_mut(&req.message_id)
        .ok_or_else(Error::message_not_found)?;

    if !lease_is_live(msg, &req.lease_token) {
        return Err(Error::lease_lost());
    }

    let lease_expires_at = now.instant + req.extension;
    msg.lease_expires_at = Some(lease_expires_at);
    Ok(RenewLeaseResponse {
        lease_expires_at: now.to_system(lease_expires_at),
    })
}

/// One result per input item, in request order. An item-level problem is an
/// item-level result and never fails the RPC (§7, batch rule), and a rejected
/// item mutates nothing.
fn do_settle_messages(
    st: &mut State,
    now: Now,
    req: SettleMessagesRequest,
) -> Result<SettleMessagesResponse> {
    let results = req
        .settlements
        .into_iter()
        .map(|s| settle_one(st, now, s))
        .collect();
    Ok(SettleMessagesResponse { results })
}

fn settle_one(st: &mut State, now: Now, s: StorageSettlement) -> SettlementResult {
    let ok = |message_id: String| SettlementResult {
        message_id,
        code: code::OK,
        error_reason: String::new(),
    };
    let failed = |message_id: String, e: Error| SettlementResult {
        message_id,
        code: e.code,
        error_reason: e.reason,
    };

    // §4's `retry_delay` bound is a property of one settlement, so it fails
    // that settlement and no other: §7's batch rule reserves RPC-level failure
    // for request-level problems. Checked only for `Retry`, the one outcome
    // the SPI says the field is meaningful for.
    if s.outcome == Outcome::Retry
        && let Err(e) = validate::retry_delay(s.retry_delay)
    {
        return failed(s.message_id, e);
    }

    let Some(queue) = st.queues.get_mut(&s.queue_id) else {
        return failed(s.message_id, Error::queue_not_found());
    };
    let Queue {
        config, messages, ..
    } = queue;
    let Some(msg) = messages.get_mut(&s.message_id) else {
        return failed(s.message_id, Error::message_not_found());
    };
    if !lease_is_live(msg, &s.lease_token) {
        return failed(s.message_id, Error::lease_lost());
    }

    match s.outcome {
        Outcome::AckDelete => {
            messages.remove(&s.message_id);
        }
        // The nack path goes through the same limit check as lease expiry
        // (design D5): a message already at `max_deliveries` dead-letters here
        // rather than becoming visible again.
        Outcome::Retry => {
            // §4 default: a `retry_delay` of zero retries immediately.
            let visible_at = now.instant + s.retry_delay;
            // A caller-supplied reason wins if there is one, so a nack that
            // happens to hit the limit records it the same way a forced
            // dead-letter would; otherwise the route itself is the reason.
            let reason = if s.dead_letter_reason.is_empty() {
                "max_deliveries exceeded"
            } else {
                &s.dead_letter_reason
            };
            if release(msg, config, visible_at, reason) == Released::Dropped {
                messages.remove(&s.message_id);
            }
        }
        // Forced, at any `delivery_count`, but still subject to
        // `dead_letter_enabled` (§3.6).
        Outcome::DeadLetter => {
            if dead_letter(msg, config, &s.dead_letter_reason) == Released::Dropped {
                messages.remove(&s.message_id);
            }
        }
        Outcome::Unspecified => {
            return failed(s.message_id, Error::validation_failed());
        }
    }

    ok(s.message_id)
}

// ---------------------------------------------------------------------------
// Maintenance
// ---------------------------------------------------------------------------

/// Administrative only: expired leases are already reclaimed by the sweep every
/// other method performs (§3.3.7). The counts reported are those of the sweep
/// this call performs, so a repeat with nothing newly expired reports `(0, 0)`.
///
/// A lease has been expired for at least `grace_period` exactly when it expired
/// at or before `now - grace_period`, so the grace period is applied by moving
/// the sweep's cutoff back rather than by a second predicate. If that instant
/// predates the process itself, nothing can have been expired that long.
fn do_reap_expired_leases(
    st: &mut State,
    now: Now,
    req: ReapExpiredLeasesRequest,
) -> Result<ReapExpiredLeasesResponse> {
    let (returned_to_ready, moved_to_dead_letter) = match now.instant.checked_sub(req.grace_period)
    {
        Some(cutoff) => sweep(st, now.instant, cutoff),
        None => (0, 0),
    };
    Ok(ReapExpiredLeasesResponse {
        returned_to_ready,
        moved_to_dead_letter,
    })
}

// ---------------------------------------------------------------------------
// The SPI. Lock, capture the clock, sweep, delegate — and nothing else.
// ---------------------------------------------------------------------------

impl StorageProvider for MemoryProvider {
    fn ensure_queue(&self, req: EnsureQueueRequest) -> Result<EnsureQueueResponse> {
        validate::ensure_queue(&req)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_ensure_queue(&mut st, req)
    }

    fn get_queue_config(&self, req: GetQueueConfigRequest) -> Result<GetQueueConfigResponse> {
        validate::queue_id(&req.queue_id)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_get_queue_config(&mut st, req)
    }

    fn list_queues(&self, req: ListQueuesRequest) -> Result<ListQueuesResponse> {
        validate::list_queues(&req)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_list_queues(&mut st, req)
    }

    fn delete_queue(&self, req: DeleteQueueRequest) -> Result<DeleteQueueResponse> {
        validate::queue_id(&req.queue_id)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_delete_queue(&mut st, req)
    }

    fn get_queue_stats(&self, req: GetQueueStatsRequest) -> Result<GetQueueStatsResponse> {
        validate::queue_id(&req.queue_id)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_get_queue_stats(&mut st, req)
    }

    fn save_message(&self, req: SaveMessageRequest) -> Result<SaveMessageResponse> {
        validate::save_message(&req)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_save_message(&mut st, now, req)
    }

    fn lease_messages(&self, req: LeaseMessagesRequest) -> Result<LeaseMessagesResponse> {
        validate::lease_messages(&req)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_lease_messages(&mut st, now, req)
    }

    fn renew_lease(&self, req: RenewLeaseRequest) -> Result<RenewLeaseResponse> {
        validate::renew_lease(&req)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_renew_lease(&mut st, now, req)
    }

    fn settle_messages(&self, req: SettleMessagesRequest) -> Result<SettleMessagesResponse> {
        validate::settle_messages(&req)?;
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        do_settle_messages(&mut st, now, req)
    }

    fn reap_expired_leases(
        &self,
        req: ReapExpiredLeasesRequest,
    ) -> Result<ReapExpiredLeasesResponse> {
        let now = Now::capture();
        let mut st = self.lock();
        // The only method whose sweep is not at `now`: its cutoff carries the
        // grace period, so it must not be pre-empted by a sweep at `now`.
        do_reap_expired_leases(&mut st, now, req)
    }

    fn get_provider_info(&self, _req: GetProviderInfoRequest) -> Result<GetProviderInfoResponse> {
        let now = Now::capture();
        let mut st = self.lock();
        sweep(&mut st, now.instant, now.instant);
        Ok(GetProviderInfoResponse {
            provider_name: "memory".to_string(),
            provider_version: env!("CARGO_PKG_VERSION").to_string(),
            contract_version: CONTRACT_VERSION.to_string(),
        })
    }
}
