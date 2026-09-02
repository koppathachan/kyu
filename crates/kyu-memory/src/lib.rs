//! In-memory Kyu v1 storage provider.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Mutex, MutexGuard};
use std::time::SystemTime;

use kyu_conformance::spi::{
    CONTRACT_VERSION, EnsureQueueRequest, EnsureQueueResponse, Error, GetProviderInfoRequest,
    GetProviderInfoResponse, GetQueueConfigRequest, GetQueueConfigResponse, LeaseMessagesRequest,
    LeaseMessagesResponse, ListQueuesRequest, ListQueuesResponse, Result, SaveMessageRequest,
    SaveMessageResponse, StorageQueueConfig,
};

/// An in-memory implementation of the Kyu v1 storage provider SPI (§6).
#[derive(Debug, Default)]
pub struct MemoryProvider {
    queues: Mutex<HashMap<String, StorageQueueConfig>>,
    next_message_id: AtomicU64,
}

impl MemoryProvider {
    pub fn new() -> Self {
        Self::default()
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
        Ok(SaveMessageResponse {
            message_id: self
                .next_message_id
                .fetch_add(1, Ordering::Relaxed)
                .to_string(),
            saved_at: SystemTime::now(),
            deduplicated: false,
        })
    }

    pub fn lease_messages(&self, req: LeaseMessagesRequest) -> Result<LeaseMessagesResponse> {
        self.require_queue(&req.queue_id)?;
        // No test demands leasing from a known queue yet.
        todo!("lease from a known queue")
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
