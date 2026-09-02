//! Protocol specification §8.1 requirement 6 — dead-lettering (§3.4, §3.6).

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, GetQueueStatsRequest, GetQueueStatsResponse, LeaseMessagesRequest,
    LeasedMessage, SaveMessageRequest, StorageQueueConfig,
};
use kyu_memory::MemoryProvider;

fn provider_with_queue(queue_id: &str, max_deliveries: i32, dead_letter: bool) -> MemoryProvider {
    let provider = MemoryProvider::new();
    provider
        .ensure_queue(EnsureQueueRequest {
            config: StorageQueueConfig {
                queue_id: queue_id.to_string(),
                default_lease_ttl: Duration::from_secs(30),
                max_deliveries,
                retry_backoff_base: Duration::from_secs(1),
                retry_backoff_max: Duration::from_secs(60),
                dedup_window: Duration::from_secs(300),
                dead_letter_enabled: dead_letter,
            },
        })
        .expect("ensure_queue");
    provider
}

fn save(provider: &MemoryProvider, queue_id: &str) {
    provider
        .save_message(SaveMessageRequest {
            queue_id: queue_id.to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
}

fn lease(provider: &MemoryProvider, queue_id: &str, ttl: Duration) -> Vec<LeasedMessage> {
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size: 10,
            lease_ttl: Some(ttl),
        })
        .expect("lease_messages")
        .messages
}

fn stats(provider: &MemoryProvider, queue_id: &str) -> GetQueueStatsResponse {
    provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: queue_id.to_string(),
        })
        .expect("get_queue_stats")
}

#[test]
fn a_message_reaching_max_deliveries_by_expiry_is_dead_lettered_and_counted() {
    let provider = provider_with_queue("orders", 1, true);
    save(&provider, "orders");
    assert_eq!(lease(&provider, "orders", Duration::from_secs(1)).len(), 1);

    std::thread::sleep(Duration::from_millis(1_100));

    assert_eq!(lease(&provider, "orders", Duration::from_secs(1)).len(), 0);
    let stats = stats(&provider, "orders");
    assert_eq!(stats.dead_letter_count, 1);
    assert_eq!(stats.ready_count, 0);
}

#[test]
fn a_dead_lettered_message_is_never_delivered_again() {
    let provider = provider_with_queue("orders", 1, true);
    save(&provider, "orders");
    lease(&provider, "orders", Duration::from_secs(1));
    std::thread::sleep(Duration::from_millis(1_100));
    lease(&provider, "orders", Duration::from_secs(1)); // sweeps it to DEAD_LETTER

    for _ in 0..3 {
        assert_eq!(lease(&provider, "orders", Duration::from_secs(1)).len(), 0);
    }
    assert_eq!(stats(&provider, "orders").dead_letter_count, 1);
}
