//! Protocol specification §8.1 requirement 2 — deduplication (§3.7).

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, GetQueueStatsRequest, SaveMessageRequest, StorageQueueConfig,
};
use kyu_memory::MemoryProvider;

fn provider_with_queue(queue_id: &str, dedup_window: Duration) -> MemoryProvider {
    let provider = MemoryProvider::new();
    provider
        .ensure_queue(EnsureQueueRequest {
            config: StorageQueueConfig {
                queue_id: queue_id.to_string(),
                default_lease_ttl: Duration::from_secs(30),
                max_deliveries: 5,
                retry_backoff_base: Duration::from_secs(1),
                retry_backoff_max: Duration::from_secs(60),
                dedup_window,
                dead_letter_enabled: true,
            },
        })
        .expect("ensure_queue");
    provider
}

fn save_with_key(provider: &MemoryProvider, queue_id: &str, key: &str) -> (String, bool) {
    let response = provider
        .save_message(SaveMessageRequest {
            queue_id: queue_id.to_string(),
            idempotency_key: key.to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    (response.message_id, response.deduplicated)
}

#[test]
fn a_first_save_with_an_idempotency_key_is_not_deduplicated() {
    let provider = provider_with_queue("orders", Duration::from_secs(300));

    let (_, deduplicated) = save_with_key(&provider, "orders", "key-1");

    assert!(!deduplicated);
}

#[test]
fn a_duplicate_save_inside_the_window_returns_the_original_message_id() {
    let provider = provider_with_queue("orders", Duration::from_secs(300));
    let (original, _) = save_with_key(&provider, "orders", "key-1");

    let (duplicate, _) = save_with_key(&provider, "orders", "key-1");

    assert_eq!(duplicate, original);
}

#[test]
fn a_duplicate_save_inside_the_window_reports_deduplicated() {
    let provider = provider_with_queue("orders", Duration::from_secs(300));
    save_with_key(&provider, "orders", "key-1");

    let (_, deduplicated) = save_with_key(&provider, "orders", "key-1");

    assert!(deduplicated);
}

#[test]
fn a_duplicate_save_inside_the_window_stores_no_second_message() {
    let provider = provider_with_queue("orders", Duration::from_secs(300));
    save_with_key(&provider, "orders", "key-1");

    save_with_key(&provider, "orders", "key-1");

    let stats = provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "orders".to_string(),
        })
        .expect("get_queue_stats");
    assert_eq!(stats.ready_count, 1);
}
