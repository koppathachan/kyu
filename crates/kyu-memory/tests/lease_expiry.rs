//! Protocol specification §8.1 requirement 3 — lease expiry.

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, LeaseMessagesRequest, LeasedMessage, SaveMessageRequest, StorageProvider,
    StorageQueueConfig,
};
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

fn lease(provider: &MemoryProvider, queue_id: &str, ttl: Duration) -> Vec<LeasedMessage> {
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size: 1,
            lease_ttl: Some(ttl),
        })
        .expect("lease_messages")
        .messages
}

#[test]
fn a_message_whose_lease_expired_is_leasable_again() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    assert_eq!(lease(&provider, "orders", Duration::from_secs(1)).len(), 1);

    std::thread::sleep(Duration::from_millis(1_100));

    assert_eq!(lease(&provider, "orders", Duration::from_secs(1)).len(), 1);
}

#[test]
fn redelivery_after_expiry_increments_delivery_count_and_rotates_the_token() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    let first = lease(&provider, "orders", Duration::from_secs(1)).remove(0);

    std::thread::sleep(Duration::from_millis(1_100));
    let second = lease(&provider, "orders", Duration::from_secs(1)).remove(0);

    assert_eq!(second.message_id, first.message_id);
    assert_eq!(second.delivery_count, 2);
    assert_ne!(second.lease_token, first.lease_token);
}
