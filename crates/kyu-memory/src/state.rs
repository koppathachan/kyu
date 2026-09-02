//! The provider's whole world: one `State`, reachable only under the mutex in
//! [`crate::MemoryProvider`].
//!
//! Design D1. `Queue::messages` is the single source of truth for a message —
//! there are no secondary indexes to drift out of sync with it — and
//! `Queue::dedup` is deliberately independent of it, because a deduplication
//! record outlives the message it names (§3.7).

use std::collections::HashMap;
use std::time::{Instant, SystemTime};

use kyu_conformance::spi::{Error, MessageState, Result, StorageQueueConfig};

/// Where a message is in its lifecycle (design D5).
///
/// Distinct from [`MessageState`] on the wire only in that it admits no
/// `Unspecified`: every stored message is in exactly one of these three, which
/// is what makes `GetQueueStats` a partition rather than three filters.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MsgState {
    Ready,
    Leased,
    DeadLetter,
}

impl From<MsgState> for MessageState {
    fn from(s: MsgState) -> Self {
        match s {
            MsgState::Ready => MessageState::Ready,
            MsgState::Leased => MessageState::Leased,
            MsgState::DeadLetter => MessageState::DeadLetter,
        }
    }
}

/// One stored message.
///
/// All times are [`Instant`]s: every comparison the provider makes is
/// monotonic, and `SystemTime` appears only on the way out (design D7).
#[derive(Debug, Clone)]
pub struct Message {
    pub id: String,
    pub payload: Vec<u8>,
    pub priority: i32,
    pub attributes: HashMap<String, String>,

    pub state: MsgState,
    /// Eligible for delivery from this instant, when `state` is `Ready`.
    pub visible_at: Instant,
    /// Includes the delivery in flight, if any (§3.4).
    pub delivery_count: i32,
    /// The token of the current delivery; `None` unless `state` is `Leased`.
    pub lease_token: Option<String>,
    /// `None` unless `state` is `Leased`.
    pub lease_expires_at: Option<Instant>,
    /// Retained but not surfaced by any v1 RPC (design D9).
    pub dead_letter_reason: String,
}

/// A `(queue_id, idempotency_key)` deduplication record (§3.7).
///
/// `created_at` is the instant of the *original* save, never of a later
/// duplicate attempt, so the window is measured from the message it names.
#[derive(Debug, Clone)]
pub struct DedupRecord {
    pub message_id: String,
    pub created_at: Instant,
}

/// One queue: its configuration, its messages, and its dedup records.
pub struct Queue {
    pub config: StorageQueueConfig,
    pub messages: HashMap<String, Message>,
    pub dedup: HashMap<String, DedupRecord>,
}

impl Queue {
    pub fn new(config: StorageQueueConfig) -> Self {
        Self {
            config,
            messages: HashMap::new(),
            dedup: HashMap::new(),
        }
    }
}

/// The provider's entire state (design D1).
#[derive(Default)]
pub struct State {
    pub queues: HashMap<String, Queue>,
    /// Monotonic source for both message ids and lease tokens.
    pub next_id: u64,
}

/// A message id: zero-padded so that lexicographic order is creation order,
/// which is the final tiebreak §3.2 asks for (design D6).
pub fn next_message_id(st: &mut State) -> String {
    st.next_id += 1;
    format!("{:020}", st.next_id)
}

/// A lease token. Drawn from the same monotonic counter, so a token is never
/// reused across deliveries (§3.3.1); the prefix keeps it from ever colliding
/// with a message id.
pub fn next_lease_token(st: &mut State) -> String {
    st.next_id += 1;
    format!("lt-{:020}", st.next_id)
}

/// The queue named by `queue_id`, or `NOT_FOUND` / `QUEUE_NOT_FOUND` (§7).
///
/// Every dereference of a `queue_id` goes through here, so an unknown queue
/// fails identically wherever it is named.
pub fn queue_mut<'a>(st: &'a mut State, queue_id: &str) -> Result<&'a mut Queue> {
    st.queues
        .get_mut(queue_id)
        .ok_or_else(Error::queue_not_found)
}

/// The pair of clock readings taken once at method entry (design D7).
///
/// `instant` drives every decision; `sys` exists only to fill response fields.
/// Holding both lets a whole response be converted from one consistent reading
/// rather than from a fresh `SystemTime::now()` per field.
#[derive(Debug, Clone, Copy)]
pub struct Now {
    pub instant: Instant,
    pub sys: SystemTime,
}

impl Now {
    pub fn capture() -> Self {
        Self {
            instant: Instant::now(),
            sys: SystemTime::now(),
        }
    }

    /// Project a stored `Instant` onto the wall clock reading taken with it.
    pub fn to_system(&self, at: Instant) -> SystemTime {
        if at >= self.instant {
            self.sys + (at - self.instant)
        } else {
            self.sys - (self.instant - at)
        }
    }
}
