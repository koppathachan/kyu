//! Protocol specification §8.1 requirement 10 — visibility delay.

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, LeaseMessagesRequest, SaveMessageRequest, StorageProvider,
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

fn lease_one(provider: &MemoryProvider, queue_id: &str) -> usize {
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size: 1,
            lease_ttl: None,
        })
        .expect("lease_messages")
        .messages
        .len()
}

#[test]
fn a_visibility_delay_holds_a_message_back_until_it_elapses() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            visibility_delay: Some(Duration::from_millis(300)),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");

    assert_eq!(
        lease_one(&provider, "orders"),
        0,
        "before the delay elapses"
    );

    std::thread::sleep(Duration::from_millis(350));
    assert_eq!(lease_one(&provider, "orders"), 1, "after the delay elapses");
}
