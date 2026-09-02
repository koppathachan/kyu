//! In-memory Kyu v1 storage provider.

use std::collections::HashMap;
use std::sync::{Mutex, MutexGuard};

use kyu_conformance::spi::{
    CONTRACT_VERSION, EnsureQueueRequest, EnsureQueueResponse, Error, GetProviderInfoRequest,
    GetProviderInfoResponse, GetQueueConfigRequest, GetQueueConfigResponse, ListQueuesRequest,
    ListQueuesResponse, Result, StorageQueueConfig,
};

/// An in-memory implementation of the Kyu v1 storage provider SPI (§6).
#[derive(Debug, Default)]
pub struct MemoryProvider {
    queues: Mutex<HashMap<String, StorageQueueConfig>>,
}

impl MemoryProvider {
    pub fn new() -> Self {
        Self::default()
    }

    fn queues(&self) -> MutexGuard<'_, HashMap<String, StorageQueueConfig>> {
        self.queues.lock().expect("queues mutex")
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
        let page: Vec<StorageQueueConfig> = queues
            .values()
            .take(if req.page_size > 0 {
                req.page_size as usize
            } else {
                usize::MAX
            })
            .cloned()
            .collect();
        Ok(ListQueuesResponse {
            queues: page,
            next_page_token: String::new(),
        })
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
