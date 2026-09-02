//! In-memory Kyu v1 storage provider.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Mutex, MutexGuard};
use std::time::SystemTime;

use kyu_conformance::spi::{
    CONTRACT_VERSION, DeleteQueueRequest, DeleteQueueResponse, EnsureQueueRequest,
    EnsureQueueResponse, Error, GetProviderInfoRequest, GetProviderInfoResponse,
    GetQueueConfigRequest, GetQueueConfigResponse, GetQueueStatsRequest, GetQueueStatsResponse,
    LeaseMessagesRequest, LeaseMessagesResponse, LeasedMessage, ListQueuesRequest,
    ListQueuesResponse, Outcome, RenewLeaseRequest, RenewLeaseResponse, Result, SaveMessageRequest,
    SaveMessageResponse, SettleMessagesRequest, SettleMessagesResponse, SettlementResult,
    StorageQueueConfig, code, reason,
};

/// An in-memory implementation of the Kyu v1 storage provider SPI (§6).
#[derive(Debug, Default)]
pub struct MemoryProvider {
    queues: Mutex<HashMap<String, StorageQueueConfig>>,
    messages: Mutex<Vec<Message>>,
    /// `(queue_id, idempotency_key)` -> the original save (§3.7).
    dedup: Mutex<HashMap<(String, String), DedupRecord>>,
    next_message_id: AtomicU64,
    next_lease_token: AtomicU64,
}

/// A message as the provider holds it.
#[derive(Debug, Clone)]
struct Message {
    id: String,
    queue_id: String,
    payload: Vec<u8>,
    priority: i32,
    attributes: HashMap<String, String>,
    delivery_count: i32,
    /// When the message becomes eligible for delivery (§3.2).
    visible_at: SystemTime,
    /// `Some` while a lease is outstanding; the instant it expires (§3.3).
    leased_until: Option<SystemTime>,
    /// The token issued by the current delivery (§3.3.1).
    lease_token: Option<String>,
    /// `DEAD_LETTER` state (§3.6). Never delivered, counted separately.
    dead_lettered: bool,
}

/// The original save behind an idempotency key. The window runs from
/// `saved_at`, not from the last duplicate attempt (§3.7).
#[derive(Debug, Clone)]
struct DedupRecord {
    message_id: String,
    saved_at: SystemTime,
}

impl Message {
    /// Whether a lease on this message is still live at `now` (§3.3.6).
    fn is_leased_at(&self, now: SystemTime) -> bool {
        self.leased_until.is_some_and(|expires_at| expires_at > now)
    }

    /// Whether the message may be delivered at `now` (§3.2).
    fn is_eligible_at(&self, now: SystemTime) -> bool {
        !self.dead_lettered && self.visible_at <= now && !self.is_leased_at(now)
    }
}

impl MemoryProvider {
    pub fn new() -> Self {
        Self::default()
    }

    fn messages(&self) -> MutexGuard<'_, Vec<Message>> {
        self.messages.lock().expect("messages mutex")
    }

    fn dedup(&self) -> MutexGuard<'_, HashMap<(String, String), DedupRecord>> {
        self.dedup.lock().expect("dedup mutex")
    }

    fn queues(&self) -> MutexGuard<'_, HashMap<String, StorageQueueConfig>> {
        self.queues.lock().expect("queues mutex")
    }

    /// Fails `QUEUE_NOT_FOUND` (§7) unless the queue has been ensured.
    fn require_queue(&self, queue_id: &str) -> Result<()> {
        self.queue_config(queue_id).map(|_| ())
    }

    /// The stored config, or `QUEUE_NOT_FOUND` (§7).
    fn queue_config(&self, queue_id: &str) -> Result<StorageQueueConfig> {
        self.queues()
            .get(queue_id)
            .cloned()
            .ok_or_else(Error::queue_not_found)
    }

    pub fn ensure_queue(&self, req: EnsureQueueRequest) -> Result<EnsureQueueResponse> {
        let mut queues = self.queues();
        let created = !queues.contains_key(&req.config.queue_id);
        let stored = queues
            .entry(req.config.queue_id.clone())
            .or_insert(req.config);
        Ok(EnsureQueueResponse {
            config: stored.clone(),
            created,
        })
    }

    pub fn get_queue_config(&self, req: GetQueueConfigRequest) -> Result<GetQueueConfigResponse> {
        let queues = self.queues();
        let config = queues
            .get(&req.queue_id)
            .cloned()
            .ok_or_else(Error::queue_not_found)?;
        Ok(GetQueueConfigResponse { config })
    }

    pub fn list_queues(&self, req: ListQueuesRequest) -> Result<ListQueuesResponse> {
        let queues = self.queues();
        let mut page: Vec<StorageQueueConfig> = queues
            .values()
            .filter(|config| config.queue_id > req.page_token)
            .cloned()
            .collect();
        page.sort_by(|a, b| a.queue_id.cmp(&b.queue_id));

        let limit = if req.page_size > 0 {
            req.page_size as usize
        } else {
            usize::MAX
        };
        let exhausted = page.len() <= limit;
        page.truncate(limit);

        let next_page_token = match page.last() {
            Some(last) if !exhausted => last.queue_id.clone(),
            _ => String::new(),
        };
        Ok(ListQueuesResponse {
            queues: page,
            next_page_token,
        })
    }

    pub fn delete_queue(&self, req: DeleteQueueRequest) -> Result<DeleteQueueResponse> {
        self.require_queue(&req.queue_id)?;
        let mut store = self.messages();
        let before = store.len();
        store.retain(|message| message.queue_id != req.queue_id);
        let purged_message_count = (before - store.len()) as i64;
        self.queues().remove(&req.queue_id);
        Ok(DeleteQueueResponse {
            purged_message_count,
        })
    }

    pub fn get_queue_stats(&self, req: GetQueueStatsRequest) -> Result<GetQueueStatsResponse> {
        self.require_queue(&req.queue_id)?;
        let now = SystemTime::now();
        let mut ready_count = 0;
        let mut leased_count = 0;
        let mut dead_letter_count = 0;
        for message in self
            .messages()
            .iter()
            .filter(|message| message.queue_id == req.queue_id)
        {
            if message.dead_lettered {
                dead_letter_count += 1;
            } else if message.is_leased_at(now) {
                leased_count += 1;
            } else {
                ready_count += 1;
            }
        }
        Ok(GetQueueStatsResponse {
            ready_count,
            leased_count,
            dead_letter_count,
        })
    }

    pub fn save_message(&self, req: SaveMessageRequest) -> Result<SaveMessageResponse> {
        let config = self.queue_config(&req.queue_id)?;
        let dedup_key = (req.queue_id.clone(), req.idempotency_key.clone());
        let saved_at = SystemTime::now();
        if !req.idempotency_key.is_empty()
            && let Some(original) = self.dedup().get(&dedup_key)
            && saved_at
                .duration_since(original.saved_at)
                .is_ok_and(|elapsed| elapsed < config.dedup_window)
        {
            return Ok(SaveMessageResponse {
                message_id: original.message_id.clone(),
                saved_at,
                deduplicated: true,
            });
        }

        // §2: ids are opaque, at most 128 chars, and SHOULD sort by creation
        // time because §3.2 uses them as the ordering tiebreak. Zero-padding
        // makes the counter sort lexicographically as well as numerically.
        let id = format!(
            "{:020}",
            self.next_message_id.fetch_add(1, Ordering::Relaxed)
        );
        if !req.idempotency_key.is_empty() {
            self.dedup().insert(
                dedup_key,
                DedupRecord {
                    message_id: id.clone(),
                    saved_at,
                },
            );
        }
        self.messages().push(Message {
            id: id.clone(),
            queue_id: req.queue_id,
            payload: req.payload,
            priority: req.priority,
            attributes: req.attributes,
            delivery_count: 0,
            visible_at: saved_at + req.visibility_delay.unwrap_or_default(),
            leased_until: None,
            lease_token: None,
            dead_lettered: false,
        });
        Ok(SaveMessageResponse {
            message_id: id,
            saved_at,
            deduplicated: false,
        })
    }

    pub fn lease_messages(&self, req: LeaseMessagesRequest) -> Result<LeaseMessagesResponse> {
        let config = self.queue_config(&req.queue_id)?;
        let lease_ttl = req.lease_ttl.unwrap_or(config.default_lease_ttl);
        let leased_at = SystemTime::now();
        let mut store = self.messages();
        // §3.4: a message at the limit is dead-lettered rather than delivered
        // again; §3.6: with dead-lettering off it is deleted instead.
        for message in store.iter_mut() {
            if message.queue_id == req.queue_id
                && message.is_eligible_at(leased_at)
                && message.delivery_count >= config.max_deliveries
            {
                message.dead_lettered = true;
            }
        }
        if !config.dead_letter_enabled {
            store.retain(|message| !message.dead_lettered);
        }

        let mut candidates: Vec<usize> = store
            .iter()
            .enumerate()
            .filter(|(_, message)| {
                message.queue_id == req.queue_id && message.is_eligible_at(leased_at)
            })
            .map(|(index, _)| index)
            .collect();
        // §3.2: priority DESC, visible_at ASC, message_id ASC.
        candidates.sort_by(|&a, &b| {
            let (a, b) = (&store[a], &store[b]);
            b.priority
                .cmp(&a.priority)
                .then(a.visible_at.cmp(&b.visible_at))
                .then(a.id.cmp(&b.id))
        });
        candidates.truncate(req.batch_size as usize);

        let messages = candidates
            .into_iter()
            .map(|index| {
                let token = self
                    .next_lease_token
                    .fetch_add(1, Ordering::Relaxed)
                    .to_string();
                let message = &mut store[index];
                message.delivery_count += 1;
                message.leased_until = Some(leased_at + lease_ttl);
                message.lease_token = Some(token.clone());
                LeasedMessage {
                    message_id: message.id.clone(),
                    queue_id: message.queue_id.clone(),
                    payload: message.payload.clone(),
                    priority: message.priority,
                    delivery_count: message.delivery_count,
                    lease_token: token,
                    lease_expires_at: leased_at + lease_ttl,
                    attributes: message.attributes.clone(),
                }
            })
            .collect();
        Ok(LeaseMessagesResponse { messages })
    }

    pub fn renew_lease(&self, req: RenewLeaseRequest) -> Result<RenewLeaseResponse> {
        let mut store = self.messages();
        let message = store
            .iter_mut()
            .find(|message| message.queue_id == req.queue_id && message.id == req.message_id)
            .ok_or_else(Error::message_not_found)?;

        // §3.3.2/§3.3.4: a stale, rotated or expired token fails LEASE_LOST and
        // leaves the message untouched. Expiry is final.
        let now = SystemTime::now();
        if !message.is_leased_at(now) || message.lease_token.as_deref() != Some(&req.lease_token) {
            return Err(Error::lease_lost());
        }

        // §3.3.3: the new expiry is now + extension, not current expiry +
        // extension, and the token is not rotated.
        let lease_expires_at = now + req.extension;
        message.leased_until = Some(lease_expires_at);
        Ok(RenewLeaseResponse { lease_expires_at })
    }

    pub fn settle_messages(&self, req: SettleMessagesRequest) -> Result<SettleMessagesResponse> {
        let now = SystemTime::now();
        let mut store = self.messages();
        let results = req
            .settlements
            .into_iter()
            .map(|settlement| {
                let config = self.queue_config(&settlement.queue_id).ok();
                // §7 batch rule: item-level problems are reported per item, and
                // a rejected item leaves its message untouched.
                let found = store.iter().position(|message| {
                    message.queue_id == settlement.queue_id && message.id == settlement.message_id
                });
                let Some(index) = found else {
                    return SettlementResult {
                        message_id: settlement.message_id,
                        code: code::NOT_FOUND,
                        error_reason: reason::MESSAGE_NOT_FOUND.to_string(),
                    };
                };
                let held = store[index].is_leased_at(now)
                    && store[index].lease_token.as_deref() == Some(&settlement.lease_token);
                if !held {
                    return SettlementResult {
                        message_id: settlement.message_id,
                        code: code::FAILED_PRECONDITION,
                        error_reason: reason::LEASE_LOST.to_string(),
                    };
                }

                let dead_letter_enabled = config.is_some_and(|config| config.dead_letter_enabled);
                match settlement.outcome {
                    Outcome::AckDelete => {
                        store.remove(index);
                    }
                    // §3.5: the engine has already applied the backoff policy;
                    // the provider just applies the delay it is given.
                    Outcome::Retry => {
                        let message = &mut store[index];
                        message.leased_until = None;
                        message.lease_token = None;
                        message.visible_at = now + settlement.retry_delay;
                    }
                    // §3.6: a forced dead-letter applies regardless of
                    // delivery_count.
                    Outcome::DeadLetter => {
                        if dead_letter_enabled {
                            let message = &mut store[index];
                            message.leased_until = None;
                            message.lease_token = None;
                            message.dead_lettered = true;
                        } else {
                            store.remove(index);
                        }
                    }
                    Outcome::Unspecified => {}
                }
                SettlementResult {
                    message_id: settlement.message_id,
                    code: code::OK,
                    error_reason: String::new(),
                }
            })
            .collect();
        Ok(SettleMessagesResponse { results })
    }

    pub fn get_provider_info(
        &self,
        _req: GetProviderInfoRequest,
    ) -> Result<GetProviderInfoResponse> {
        Ok(GetProviderInfoResponse {
            provider_name: "memory".to_string(),
            provider_version: env!("CARGO_PKG_VERSION").to_string(),
            contract_version: CONTRACT_VERSION.to_string(),
        })
    }
}
