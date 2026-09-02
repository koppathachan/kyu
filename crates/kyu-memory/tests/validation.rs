//! Protocol specification §4 — limits and validation.

use std::time::Duration;

use kyu_conformance::spi::{EnsureQueueRequest, Error, StorageQueueConfig};
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
fn an_empty_or_oversized_queue_id_fails_validation() {
    let provider = MemoryProvider::new();

    for queue_id in ["", &"q".repeat(257)] {
        let error = provider
            .ensure_queue(EnsureQueueRequest {
                config: config(queue_id),
            })
            .expect_err("invalid queue_id");
        assert_eq!(error, Error::validation_failed(), "queue_id {queue_id:?}");
    }

    // 256 bytes is the limit, not one past it.
    provider
        .ensure_queue(EnsureQueueRequest {
            config: config(&"q".repeat(256)),
        })
        .expect("256 bytes is legal");
}
