//! Protocol specification §8.1 requirement 1 — save → lease → ack.

use std::time::Duration;

use kyu_conformance::spi::{EnsureQueueRequest, SaveMessageRequest, StorageQueueConfig};
use kyu_memory::MemoryProvider;

fn provider_with_queue(queue_id: &str) -> MemoryProvider {
    let provider = MemoryProvider::new();
    provider
        .ensure_queue(EnsureQueueRequest {
            config: StorageQueueConfig {
                queue_id: queue_id.to_string(),
                default_lease_ttl: Duration::from_secs(30),
                max_deliveries: 5,
                retry_backoff_base: Duration::from_secs(1),
                retry_backoff_max: Duration::from_secs(60),
                dedup_window: Duration::from_secs(300),
                dead_letter_enabled: true,
            },
        })
        .expect("ensure_queue");
    provider
}

#[test]
fn saving_a_message_returns_a_message_id() {
    let provider = provider_with_queue("orders");

    let response = provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            payload: b"hello".to_vec(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");

    assert!(!response.message_id.is_empty());
}
