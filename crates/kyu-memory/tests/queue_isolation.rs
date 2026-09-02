//! Protocol specification §8.1 requirement 11 — queue isolation.

use std::time::Duration;

use kyu_conformance::spi::{
    DeleteQueueRequest, EnsureQueueRequest, GetQueueStatsRequest, LeaseMessagesRequest,
    SaveMessageRequest, StorageQueueConfig,
};
use kyu_memory::MemoryProvider;

fn provider_with_queues(queue_ids: &[&str]) -> MemoryProvider {
    let provider = MemoryProvider::new();
    for queue_id in queue_ids {
        provider
            .ensure_queue(EnsureQueueRequest {
                config: StorageQueueConfig {
                    queue_id: (*queue_id).to_string(),
                    default_lease_ttl: Duration::from_secs(30),
                    max_deliveries: 5,
                    retry_backoff_base: Duration::from_secs(1),
                    retry_backoff_max: Duration::from_secs(60),
                    dedup_window: Duration::from_secs(300),
                    dead_letter_enabled: true,
                },
            })
            .expect("ensure_queue");
    }
    provider
}

fn save(provider: &MemoryProvider, queue_id: &str, payload: &[u8]) {
    provider
        .save_message(SaveMessageRequest {
            queue_id: queue_id.to_string(),
            payload: payload.to_vec(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
}

#[test]
fn leasing_from_one_queue_never_returns_another_queues_messages() {
    let provider = provider_with_queues(&["orders", "payments"]);
    save(&provider, "orders", b"order");
    save(&provider, "payments", b"payment");

    let leased = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 10,
            lease_ttl: None,
        })
        .expect("lease_messages")
        .messages;

    assert_eq!(leased.len(), 1);
    assert_eq!(leased[0].queue_id, "orders");
    assert_eq!(leased[0].payload, b"order");
}

#[test]
fn deleting_a_queue_removes_its_messages_and_reports_the_purged_count() {
    let provider = provider_with_queues(&["orders"]);
    save(&provider, "orders", b"one");
    save(&provider, "orders", b"two");

    let response = provider
        .delete_queue(DeleteQueueRequest {
            queue_id: "orders".to_string(),
        })
        .expect("delete_queue");

    assert_eq!(response.purged_message_count, 2);
    provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "orders".to_string(),
        })
        .expect_err("the queue is gone");
}

#[test]
fn deleting_a_queue_leaves_another_queues_messages_intact() {
    let provider = provider_with_queues(&["orders", "payments"]);
    save(&provider, "orders", b"order");
    save(&provider, "payments", b"payment");

    provider
        .delete_queue(DeleteQueueRequest {
            queue_id: "orders".to_string(),
        })
        .expect("delete_queue");

    let stats = provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "payments".to_string(),
        })
        .expect("get_queue_stats");
    assert_eq!(stats.ready_count, 1);
}
