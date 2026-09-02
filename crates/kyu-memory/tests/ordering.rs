//! Protocol specification §8.1 requirement 7 — priority and ordering (§3.2).

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, LeaseMessagesRequest, SaveMessageRequest, StorageQueueConfig,
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

fn save(provider: &MemoryProvider, queue_id: &str, priority: i32) -> String {
    provider
        .save_message(SaveMessageRequest {
            queue_id: queue_id.to_string(),
            priority,
            ..SaveMessageRequest::default()
        })
        .expect("save_message")
        .message_id
}

#[test]
fn message_ids_are_lexicographically_ordered_by_creation_time() {
    let provider = provider_with_queue("orders");

    // §2: message_id SHOULD sort by creation time, because §3.2 uses it as the
    // ordering tiebreak. A plain decimal counter does not: "10" < "9".
    let ids: Vec<String> = (0..12).map(|_| save(&provider, "orders", 0)).collect();

    let mut sorted = ids.clone();
    sorted.sort();
    assert_eq!(ids, sorted);
}

fn lease_ids(provider: &MemoryProvider, queue_id: &str, batch_size: i32) -> Vec<String> {
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size,
            lease_ttl: None,
        })
        .expect("lease_messages")
        .messages
        .into_iter()
        .map(|message| message.message_id)
        .collect()
}

#[test]
fn the_highest_priority_visible_message_is_delivered_first() {
    let provider = provider_with_queue("orders");
    let low = save(&provider, "orders", 1);
    let high = save(&provider, "orders", 9);

    let delivered = lease_ids(&provider, "orders", 2);

    assert_eq!(delivered, [high, low]);
}
