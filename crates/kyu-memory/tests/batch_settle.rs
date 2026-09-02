//! Protocol specification §8.1 requirement 9 — batch settle (§7 batch rule).

use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, GetQueueStatsRequest, LeaseMessagesRequest, LeasedMessage, Outcome,
    SaveMessageRequest, SettleMessagesRequest, StorageProvider, StorageQueueConfig,
    StorageSettlement, code, reason,
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

fn save_and_lease(provider: &MemoryProvider, queue_id: &str, count: usize) -> Vec<LeasedMessage> {
    for _ in 0..count {
        provider
            .save_message(SaveMessageRequest {
                queue_id: queue_id.to_string(),
                ..SaveMessageRequest::default()
            })
            .expect("save_message");
    }
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size: count as i32,
            lease_ttl: Some(Duration::from_secs(30)),
        })
        .expect("lease_messages")
        .messages
}

fn ack(leased: &LeasedMessage) -> StorageSettlement {
    StorageSettlement {
        queue_id: leased.queue_id.clone(),
        message_id: leased.message_id.clone(),
        lease_token: leased.lease_token.clone(),
        outcome: Outcome::AckDelete,
        retry_delay: Duration::ZERO,
        dead_letter_reason: String::new(),
    }
}

#[test]
fn settling_returns_one_result_per_settlement_in_request_order() {
    let provider = provider_with_queue("orders");
    let leased = save_and_lease(&provider, "orders", 3);
    let settlements: Vec<StorageSettlement> = leased.iter().rev().map(ack).collect();
    let expected: Vec<String> = settlements
        .iter()
        .map(|settlement| settlement.message_id.clone())
        .collect();

    let response = provider
        .settle_messages(SettleMessagesRequest { settlements })
        .expect("settle_messages");

    let returned: Vec<String> = response
        .results
        .into_iter()
        .map(|result| result.message_id)
        .collect();
    assert_eq!(returned, expected);
}

#[test]
fn a_mixed_batch_applies_the_valid_item_and_leaves_the_stale_one_alone() {
    let provider = provider_with_queue("orders");
    let leased = save_and_lease(&provider, "orders", 2);
    let (valid, untouched) = (&leased[0], &leased[1]);
    let mut stale = ack(untouched);
    stale.lease_token = "not-the-current-token".to_string();

    let response = provider
        .settle_messages(SettleMessagesRequest {
            settlements: vec![ack(valid), stale],
        })
        .expect("settle_messages");

    assert_eq!(response.results[0].code, code::OK);
    assert_eq!(response.results[1].code, code::FAILED_PRECONDITION);
    assert_eq!(response.results[1].error_reason, reason::LEASE_LOST);

    // The valid ack deleted its message; the stale one is still leased.
    let stats = provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "orders".to_string(),
        })
        .expect("get_queue_stats");
    assert_eq!(stats.leased_count, 1);
    assert_eq!(stats.ready_count, 0);
}

#[test]
fn settling_a_message_that_does_not_exist_reports_message_not_found() {
    let provider = provider_with_queue("orders");

    let response = provider
        .settle_messages(SettleMessagesRequest {
            settlements: vec![StorageSettlement {
                queue_id: "orders".to_string(),
                message_id: "no-such-message".to_string(),
                lease_token: "no-such-token".to_string(),
                outcome: Outcome::AckDelete,
                retry_delay: Duration::ZERO,
                dead_letter_reason: String::new(),
            }],
        })
        .expect("settle_messages");

    assert_eq!(response.results[0].code, code::NOT_FOUND);
    assert_eq!(response.results[0].error_reason, reason::MESSAGE_NOT_FOUND);
}
