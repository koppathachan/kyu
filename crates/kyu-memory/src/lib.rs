//! In-memory Kyu v1 storage provider.

use std::collections::HashMap;
use std::sync::Mutex;

use kyu_conformance::spi::{
    CONTRACT_VERSION, EnsureQueueRequest, EnsureQueueResponse, GetProviderInfoRequest,
    GetProviderInfoResponse, Result, StorageQueueConfig,
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

    pub fn ensure_queue(&self, req: EnsureQueueRequest) -> Result<EnsureQueueResponse> {
        let mut queues = self.queues.lock().expect("queues mutex");
        let created = !queues.contains_key(&req.config.queue_id);
        if created {
            queues.insert(req.config.queue_id.clone(), req.config.clone());
        }
        Ok(EnsureQueueResponse {
            config: req.config,
            created,
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
