//! Request validation — protocol specification §4, with the one documented
//! exception recorded in design D8.
//!
//! Everything here returns `INVALID_ARGUMENT` / `VALIDATION_FAILED` (§7).
//! Limits that §4 states as *defaults* rather than bounds are field semantics
//! and live at their use sites, not here.

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, Error, LeaseMessagesRequest, ListQueuesRequest, RenewLeaseRequest, Result,
    SaveMessageRequest, SettleMessagesRequest,
};

const MAX_PAYLOAD: usize = 1024 * 1024;
const MAX_ATTRIBUTES: usize = 16;
const MAX_ATTRIBUTES_BYTES: usize = 4 * 1024;
const MAX_IDEMPOTENCY_KEY: usize = 256;
const MAX_QUEUE_ID: usize = 256;
const MAX_SETTLEMENTS: usize = 1000;
const MAX_BATCH_SIZE: i32 = 100;
const TWELVE_HOURS: Duration = Duration::from_secs(12 * 60 * 60);

fn fail<T>() -> Result<T> {
    Err(Error::validation_failed())
}

/// `queue_id`: non-empty, at most 256 bytes.
pub fn queue_id(id: &str) -> Result<()> {
    if id.is_empty() || id.len() > MAX_QUEUE_ID {
        return fail();
    }
    Ok(())
}

/// A lease TTL or a `RenewLease.extension`.
///
/// The §4 **maximum** of 12 hours is enforced. The §4 **minimum of 1 second is
/// deliberately not enforced** (design D8): §4's table is written in §5's
/// client-API vocabulary and binds a provider only by analogy, and the §8.1
/// scenarios `r03_lease_expiry`, `r04_stale_token`, `r06_dead_lettering` and
/// `r12_reaping` all lease with a 300 ms TTL. Where the analogy contradicts the
/// fixed oracle, §8.1 governs.
fn lease_duration(d: Duration) -> Result<()> {
    if d > TWELVE_HOURS {
        return fail();
    }
    Ok(())
}

pub fn ensure_queue(req: &EnsureQueueRequest) -> Result<()> {
    queue_id(&req.config.queue_id)?;
    if req.config.max_deliveries < 1 {
        return fail();
    }
    Ok(())
}

pub fn save_message(req: &SaveMessageRequest) -> Result<()> {
    queue_id(&req.queue_id)?;
    if req.payload.len() > MAX_PAYLOAD {
        return fail();
    }
    if req.attributes.len() > MAX_ATTRIBUTES {
        return fail();
    }
    // "4 KiB serialised" measured as the payload bytes themselves — the sum of
    // every key and value — with no allowance for encoding framing, which the
    // SPI does not specify.
    let attribute_bytes: usize = req.attributes.iter().map(|(k, v)| k.len() + v.len()).sum();
    if attribute_bytes > MAX_ATTRIBUTES_BYTES {
        return fail();
    }
    if req.idempotency_key.len() > MAX_IDEMPOTENCY_KEY {
        return fail();
    }
    Ok(())
}

pub fn lease_messages(req: &LeaseMessagesRequest) -> Result<()> {
    queue_id(&req.queue_id)?;
    if req.batch_size < 0 || req.batch_size > MAX_BATCH_SIZE {
        return fail();
    }
    if let Some(ttl) = req.lease_ttl {
        lease_duration(ttl)?;
    }
    Ok(())
}

pub fn renew_lease(req: &RenewLeaseRequest) -> Result<()> {
    queue_id(&req.queue_id)?;
    lease_duration(req.extension)
}

/// Request-level validation only: the batch's own shape.
///
/// Per-item limits are deliberately absent. §7's batch rule reserves RPC-level
/// failure for request-level problems, so one malformed settlement must not
/// discard the valid ones alongside it; see [`retry_delay`], applied per item
/// in `settle_one`.
pub fn settle_messages(req: &SettleMessagesRequest) -> Result<()> {
    if req.settlements.is_empty() || req.settlements.len() > MAX_SETTLEMENTS {
        return fail();
    }
    Ok(())
}

/// One settlement's `retry_delay`. Applied per item, never to the batch.
pub fn retry_delay(d: Duration) -> Result<()> {
    if d > TWELVE_HOURS {
        return fail();
    }
    Ok(())
}

/// `ListQueues.page_size`: §4 sets no upper bound, but a negative count is
/// meaningless. Rejected rather than silently read as "all", which is what a
/// zero means.
pub fn list_queues(req: &ListQueuesRequest) -> Result<()> {
    if req.page_size < 0 {
        return fail();
    }
    Ok(())
}
