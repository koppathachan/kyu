//! The queue registry (§6) — prerequisite for §8.1 requirements 1 and 11.

use std::time::Duration;

use kyu_conformance::spi::{EnsureQueueRequest, StorageQueueConfig};
use kyu_memory::MemoryProvider;

fn config(queue_id: &str) -> StorageQueueConfig {
    StorageQueueConfig {
        queue_id: queue_id.to_string(),
        default_lease_ttl: Duration::from_secs(30),
        max_deliveries: 5,
        retry_backoff_base: Duration::from_secs(1),
        retry_backoff_max: Duration::from_secs(60),
        dedup_window: Duration::from_secs(300),
        dead_letter_enabled: true,
    }
}

#[test]
fn ensuring_a_new_queue_reports_it_was_created() {
    let provider = MemoryProvider::new();

    let response = provider
        .ensure_queue(EnsureQueueRequest {
            config: config("orders"),
        })
        .expect("ensure_queue");

    assert!(response.created);
}
