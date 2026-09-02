//! The queue registry (§6) — prerequisite for §8.1 requirements 1 and 11.

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, Error, GetQueueConfigRequest, ListQueuesRequest, SaveMessageRequest,
    StorageQueueConfig,
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

#[test]
fn listing_queues_returns_the_queues_that_were_ensured() {
    let provider = MemoryProvider::new();
    for queue_id in ["orders", "payments"] {
        provider
            .ensure_queue(EnsureQueueRequest {
                config: config(queue_id),
            })
            .expect("ensure_queue");
    }

    let response = provider
        .list_queues(ListQueuesRequest::default())
        .expect("list_queues");

    let mut listed: Vec<String> = response.queues.into_iter().map(|c| c.queue_id).collect();
    listed.sort();
    assert_eq!(listed, ["orders", "payments"]);
}

#[test]
fn listing_queues_returns_no_more_than_the_page_size() {
    let provider = MemoryProvider::new();
    for queue_id in ["orders", "payments", "shipments"] {
        provider
            .ensure_queue(EnsureQueueRequest {
                config: config(queue_id),
            })
            .expect("ensure_queue");
    }

    let response = provider
        .list_queues(ListQueuesRequest {
            page_size: 2,
            page_token: String::new(),
        })
        .expect("list_queues");

    assert_eq!(response.queues.len(), 2);
}

#[test]
fn listing_queues_continues_from_the_previous_pages_token() {
    let provider = MemoryProvider::new();
    for queue_id in ["orders", "payments", "shipments"] {
        provider
            .ensure_queue(EnsureQueueRequest {
                config: config(queue_id),
            })
            .expect("ensure_queue");
    }

    let first = provider
        .list_queues(ListQueuesRequest {
            page_size: 2,
            page_token: String::new(),
        })
        .expect("first page");
    let second = provider
        .list_queues(ListQueuesRequest {
            page_size: 2,
            page_token: first.next_page_token.clone(),
        })
        .expect("second page");

    let mut seen: Vec<String> = first
        .queues
        .into_iter()
        .chain(second.queues)
        .map(|c| c.queue_id)
        .collect();
    seen.sort();
    assert_eq!(seen, ["orders", "payments", "shipments"]);
}

#[test]
fn saving_to_an_unknown_queue_fails_queue_not_found() {
    let provider = MemoryProvider::new();

    let error = provider
        .save_message(SaveMessageRequest {
            queue_id: "nonexistent".to_string(),
            ..SaveMessageRequest::default()
        })
        .expect_err("unknown queue");

    assert_eq!(error, Error::queue_not_found());
}
