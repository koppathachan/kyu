//! The queue registry (§6) — prerequisite for §8.1 requirements 1 and 11.

use std::time::Duration;

use kyu_conformance::spi::{EnsureQueueRequest, Error, GetQueueConfigRequest, StorageQueueConfig};
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

#[test]
fn ensuring_an_existing_queue_reports_it_was_not_created() {
    let provider = MemoryProvider::new();
    provider
        .ensure_queue(EnsureQueueRequest {
            config: config("orders"),
        })
        .expect("first ensure_queue");

    let response = provider
        .ensure_queue(EnsureQueueRequest {
            config: config("orders"),
        })
        .expect("second ensure_queue");

    assert!(!response.created);
}

#[test]
fn ensuring_an_existing_queue_returns_the_stored_config() {
    let provider = MemoryProvider::new();
    let stored = config("orders");
    provider
        .ensure_queue(EnsureQueueRequest {
            config: stored.clone(),
        })
        .expect("first ensure_queue");

    let mut different = config("orders");
    different.max_deliveries = 99;
    let response = provider
        .ensure_queue(EnsureQueueRequest { config: different })
        .expect("second ensure_queue");

    assert_eq!(response.config, stored);
}

#[test]
fn getting_a_queues_config_returns_what_it_was_ensured_with() {
    let provider = MemoryProvider::new();
    let stored = config("orders");
    provider
        .ensure_queue(EnsureQueueRequest {
            config: stored.clone(),
        })
        .expect("ensure_queue");

    let response = provider
        .get_queue_config(GetQueueConfigRequest {
            queue_id: "orders".to_string(),
        })
        .expect("get_queue_config");

    assert_eq!(response.config, stored);
}

#[test]
fn getting_the_config_of_an_unknown_queue_fails_queue_not_found() {
    let provider = MemoryProvider::new();

    let error = provider
        .get_queue_config(GetQueueConfigRequest {
            queue_id: "nonexistent".to_string(),
        })
        .expect_err("unknown queue");

    assert_eq!(error, Error::queue_not_found());
}
