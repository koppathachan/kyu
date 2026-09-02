//! Reclaiming expired leases, and the one place `max_deliveries` is enforced.
//!
//! Design D5: a nack and a lease expiry are the same rule applied at the same
//! moment — *when a message would become eligible for delivery again*. Both go
//! through [`release`], so §3.4's "MUST NOT be delivered again" is structural
//! rather than a check that could be forgotten at one call site.
//!
//! Design D3: [`sweep`] is the first act of every public method, so no method
//! ever observes a lease that has already expired.

use std::time::Instant;

use kyu_conformance::spi::StorageQueueConfig;

use crate::state::{Message, MsgState, State};

/// What became of a message that was about to become eligible again.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Released {
    /// Below `max_deliveries`: back to `READY`.
    Ready,
    /// At or past `max_deliveries`, or forced: moved to `DEAD_LETTER`.
    DeadLettered,
    /// As `DeadLettered`, but `dead_letter_enabled` is false, so the caller
    /// must remove the message entirely (§3.6).
    Dropped,
}

/// Apply design D5's bottom half to one message.
///
/// `visible_at` is where the message lands if it goes back to `READY`: `now`
/// for a lease expiry, `now + retry_delay` for a nack.
pub fn release(
    msg: &mut Message,
    config: &StorageQueueConfig,
    visible_at: Instant,
    reason: &str,
) -> Released {
    msg.lease_token = None;
    msg.lease_expires_at = None;

    if msg.delivery_count >= config.max_deliveries {
        dead_letter(msg, config, reason)
    } else {
        msg.state = MsgState::Ready;
        msg.visible_at = visible_at;
        Released::Ready
    }
}

/// Move a message to `DEAD_LETTER`, or drop it when the queue has
/// dead-lettering disabled (§3.6). Used by [`release`] and by a forced
/// `OUTCOME_DEAD_LETTER` settlement, which reaches it regardless of
/// `delivery_count`.
pub fn dead_letter(msg: &mut Message, config: &StorageQueueConfig, reason: &str) -> Released {
    msg.lease_token = None;
    msg.lease_expires_at = None;

    if !config.dead_letter_enabled {
        return Released::Dropped;
    }
    msg.state = MsgState::DeadLetter;
    if !reason.is_empty() {
        msg.dead_letter_reason = reason.to_string();
    }
    Released::DeadLettered
}

/// Reclaim every lease that had expired by `cutoff`, across all queues.
///
/// `now` is the provider's current instant; released messages become visible
/// at it. `cutoff` is the expiry horizon, which is `now` everywhere except in
/// `ReapExpiredLeases`, where it is `now - grace_period`.
///
/// Returns `(returned_to_ready, moved_to_dead_letter)`. A message dropped
/// because its queue has `dead_letter_enabled = false` counts under
/// `moved_to_dead_letter`: it took the dead-letter route, and it certainly did
/// not return to ready.
///
/// Idempotent: with nothing expired it mutates nothing and returns `(0, 0)`.
pub fn sweep(st: &mut State, now: Instant, cutoff: Instant) -> (i64, i64) {
    let mut returned_to_ready = 0;
    let mut moved_to_dead_letter = 0;

    for queue in st.queues.values_mut() {
        let mut dropped: Vec<String> = Vec::new();

        for msg in queue.messages.values_mut() {
            if msg.state != MsgState::Leased {
                continue;
            }
            let Some(expires_at) = msg.lease_expires_at else {
                continue;
            };
            if expires_at > cutoff {
                continue;
            }

            match release(msg, &queue.config, now, "max_deliveries exceeded") {
                Released::Ready => returned_to_ready += 1,
                Released::DeadLettered => moved_to_dead_letter += 1,
                Released::Dropped => {
                    moved_to_dead_letter += 1;
                    dropped.push(msg.id.clone());
                }
            }
        }

        for id in dropped {
            queue.messages.remove(&id);
        }
    }

    (returned_to_ready, moved_to_dead_letter)
}
