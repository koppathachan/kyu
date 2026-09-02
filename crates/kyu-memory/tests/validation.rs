//! Protocol specification §4 — limits and validation.

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, Error, SaveMessageRequest, StorageQueueConfig,
};
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

#[test]
fn max_deliveries_below_one_fails_validation() {
    let provider = MemoryProvider::new();

    for max_deliveries in [0, -1] {
        let mut invalid = config("orders");
        invalid.max_deliveries = max_deliveries;
        let error = provider
            .ensure_queue(EnsureQueueRequest { config: invalid })
            .expect_err("max_deliveries must be >= 1");
        assert_eq!(error, Error::validation_failed(), "{max_deliveries}");
    }
}

fn provider_with_queue(queue_id: &str) -> MemoryProvider {
    let provider = MemoryProvider::new();
    provider
        .ensure_queue(EnsureQueueRequest {
            config: config(queue_id),
        })
        .expect("ensure_queue");
    provider
}

#[test]
fn an_empty_payload_is_accepted() {
    let provider = provider_with_queue("orders");

    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            payload: Vec::new(),
            ..SaveMessageRequest::default()
        })
        .expect("§4: an empty payload is legal");
}
