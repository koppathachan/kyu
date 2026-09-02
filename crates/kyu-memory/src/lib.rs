//! In-memory Kyu v1 storage provider.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Mutex, MutexGuard};
use std::time::SystemTime;

use kyu_conformance::spi::{
    CONTRACT_VERSION, EnsureQueueRequest, EnsureQueueResponse, Error, GetProviderInfoRequest,
    GetProviderInfoResponse, GetQueueConfigRequest, GetQueueConfigResponse, LeaseMessagesRequest,
    LeaseMessagesResponse, LeasedMessage, ListQueuesRequest, ListQueuesResponse, Result,
    SaveMessageRequest, SaveMessageResponse, StorageQueueConfig,
};

/// An in-memory implementation of the Kyu v1 storage provider SPI (§6).
#[derive(Debug, Default)]
pub struct MemoryProvider {
    queues: Mutex<HashMap<String, StorageQueueConfig>>,
    messages: Mutex<Vec<Message>>,
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
}

impl MemoryProvider {
    pub fn new() -> Self {
        Self::default()
    }

    fn messages(&self) -> MutexGuard<'_, Vec<Message>> {
        self.messages.lock().expect("messages mutex")
    }

    fn queues(&self) -> MutexGuard<'_, HashMap<String, StorageQueueConfig>> {
        self.queues.lock().expect("queues mutex")
    }

    /// Fails `QUEUE_NOT_FOUND` (§7) unless the queue has been ensured.
    fn require_queue(&self, queue_id: &str) -> Result<()> {
        if self.queues().contains_key(queue_id) {
            Ok(())
        } else {
            Err(Error::queue_not_found())
        }
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

    pub fn save_message(&self, req: SaveMessageRequest) -> Result<SaveMessageResponse> {
        self.require_queue(&req.queue_id)?;
        let id = self
            .next_message_id
            .fetch_add(1, Ordering::Relaxed)
            .to_string();
        self.messages().push(Message {
            id: id.clone(),
            queue_id: req.queue_id,
            payload: req.payload,
            priority: req.priority,
            attributes: req.attributes,
            delivery_count: 0,
        });
        Ok(SaveMessageResponse {
            message_id: id,
            saved_at: SystemTime::now(),
            deduplicated: false,
        })
    }

    pub fn lease_messages(&self, req: LeaseMessagesRequest) -> Result<LeaseMessagesResponse> {
        self.require_queue(&req.queue_id)?;
        // The queue default is the next test; until then an omitted ttl is zero.
        let lease_ttl = req.lease_ttl.unwrap_or_default();
        let leased_at = SystemTime::now();
        let next_lease_token = &self.next_lease_token;
        let messages = self
            .messages()
            .iter_mut()
            .filter(|message| message.queue_id == req.queue_id)
            .map(|message| {
                message.delivery_count += 1;
                LeasedMessage {
                    message_id: message.id.clone(),
                    queue_id: message.queue_id.clone(),
                    payload: message.payload.clone(),
                    priority: message.priority,
                    delivery_count: message.delivery_count,
                    lease_token: next_lease_token.fetch_add(1, Ordering::Relaxed).to_string(),
                    lease_expires_at: leased_at + lease_ttl,
                    attributes: message.attributes.clone(),
                }
            })
            .collect();
        Ok(LeaseMessagesResponse { messages })
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
