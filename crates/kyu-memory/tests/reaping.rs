//! Protocol specification §8.1 requirement 12 — reaping (§3.3.7).

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, GetQueueStatsRequest, GetQueueStatsResponse, LeaseMessagesRequest,
    ReapExpiredLeasesRequest, SaveMessageRequest, StorageQueueConfig,
};
use kyu_memory::MemoryProvider;

fn provider_with_queue(queue_id: &str, max_deliveries: i32) -> MemoryProvider {
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
                dead_letter_enabled: true,
            },
        })
        .expect("ensure_queue");
    provider
}

fn save_and_lease(provider: &MemoryProvider, queue_id: &str, count: usize, ttl: Duration) {
    for _ in 0..count {
        provider
            .save_message(SaveMessageRequest {
                queue_id: queue_id.to_string(),
                ..SaveMessageRequest::default()
            })
            .expect("save_message");
    }
    let leased = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size: count as i32,
            lease_ttl: Some(ttl),
        })
        .expect("lease_messages")
        .messages;
    assert_eq!(leased.len(), count);
}

fn stats(provider: &MemoryProvider, queue_id: &str) -> GetQueueStatsResponse {
    provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: queue_id.to_string(),
        })
        .expect("get_queue_stats")
}

#[test]
fn an_expired_lease_is_ready_again_without_any_call_to_reap() {
    let provider = provider_with_queue("orders", 5);
    save_and_lease(&provider, "orders", 1, Duration::from_secs(1));
    assert_eq!(stats(&provider, "orders").leased_count, 1);

    std::thread::sleep(Duration::from_millis(1_100));

    // §3.3.7: the engine never has to call ReapExpiredLeases for correctness.
    let stats = stats(&provider, "orders");
    assert_eq!(stats.ready_count, 1);
    assert_eq!(stats.leased_count, 0);
}

#[test]
fn reap_reports_how_many_leases_it_returned_to_ready() {
    let provider = provider_with_queue("orders", 5);
    save_and_lease(&provider, "orders", 3, Duration::from_secs(1));

    std::thread::sleep(Duration::from_millis(1_100));
    let reaped = provider
        .reap_expired_leases(ReapExpiredLeasesRequest {
            grace_period: Duration::ZERO,
        })
        .expect("reap_expired_leases");

    assert_eq!(reaped.returned_to_ready, 3);
    assert_eq!(reaped.moved_to_dead_letter, 0);
}

#[test]
fn reap_reports_how_many_messages_it_moved_to_dead_letter() {
    // max_deliveries 1: one delivery already puts these at the limit.
    let provider = provider_with_queue("orders", 1);
    save_and_lease(&provider, "orders", 2, Duration::from_secs(1));

    std::thread::sleep(Duration::from_millis(1_100));
    let reaped = provider
        .reap_expired_leases(ReapExpiredLeasesRequest {
            grace_period: Duration::ZERO,
        })
        .expect("reap_expired_leases");

    assert_eq!(reaped.moved_to_dead_letter, 2);
    assert_eq!(reaped.returned_to_ready, 0);
    assert_eq!(stats(&provider, "orders").dead_letter_count, 2);
}
