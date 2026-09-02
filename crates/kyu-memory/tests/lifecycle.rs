//! Protocol specification §8.1 requirement 1 — save → lease → ack.

use std::collections::HashMap;
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

#[test]
fn saving_a_message_returns_a_message_id() {
    let provider = provider_with_queue("orders");

    let response = provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            payload: b"hello".to_vec(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");

    assert!(!response.message_id.is_empty());
}

#[test]
fn leasing_from_an_empty_queue_returns_no_messages() {
    let provider = provider_with_queue("orders");

    let response = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: None,
        })
        .expect("lease_messages");

    assert!(response.messages.is_empty());
}

#[test]
fn leasing_returns_a_saved_message_with_its_payload() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            payload: b"hello".to_vec(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");

    let response = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: None,
        })
        .expect("lease_messages");

    assert_eq!(response.messages.len(), 1);
    assert_eq!(response.messages[0].payload, b"hello");
}

#[test]
fn a_leased_message_carries_the_attributes_it_was_saved_with() {
    let provider = provider_with_queue("orders");
    let attributes = HashMap::from([("trace".to_string(), "abc123".to_string())]);
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            attributes: attributes.clone(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");

    let response = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: None,
        })
        .expect("lease_messages");

    assert_eq!(response.messages[0].attributes, attributes);
}
