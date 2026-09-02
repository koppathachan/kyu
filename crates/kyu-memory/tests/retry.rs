//! Protocol specification §8.1 requirement 5 — retry delay (§3.5).

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, LeaseMessagesRequest, LeasedMessage, Outcome, SaveMessageRequest,
    SettleMessagesRequest, StorageQueueConfig, StorageSettlement,
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

fn lease(provider: &MemoryProvider, queue_id: &str) -> Vec<LeasedMessage> {
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size: 1,
            lease_ttl: Some(Duration::from_secs(30)),
        })
        .expect("lease_messages")
        .messages
}

fn nack(provider: &MemoryProvider, leased: &LeasedMessage, retry_delay: Duration) {
    let response = provider
        .settle_messages(SettleMessagesRequest {
            settlements: vec![StorageSettlement {
                queue_id: leased.queue_id.clone(),
                message_id: leased.message_id.clone(),
                lease_token: leased.lease_token.clone(),
                outcome: Outcome::Retry,
                retry_delay,
                dead_letter_reason: String::new(),
            }],
        })
        .expect("settle_messages");
    assert_eq!(response.results[0].code, 0, "nack should succeed");
}

#[test]
fn a_nack_with_a_retry_delay_holds_the_message_back_until_it_elapses() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    let leased = lease(&provider, "orders").remove(0);

    nack(&provider, &leased, Duration::from_millis(300));

    assert_eq!(lease(&provider, "orders").len(), 0, "during the delay");
    std::thread::sleep(Duration::from_millis(350));
    assert_eq!(lease(&provider, "orders").len(), 1, "after the delay");
}

#[test]
fn a_nack_with_a_zero_retry_delay_makes_the_message_leasable_immediately() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    let leased = lease(&provider, "orders").remove(0);

    nack(&provider, &leased, Duration::ZERO);

    let redelivered = lease(&provider, "orders");
    assert_eq!(redelivered.len(), 1);
    assert_eq!(redelivered[0].delivery_count, 2);
}
