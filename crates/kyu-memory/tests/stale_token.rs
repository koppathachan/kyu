//! Protocol specification §8.1 requirement 4 — stale tokens (§3.3).

use std::time::{Duration, SystemTime};

use kyu_conformance::spi::{
    EnsureQueueRequest, Error, GetQueueStatsRequest, LeaseMessagesRequest, LeasedMessage, Outcome,
    RenewLeaseRequest, SaveMessageRequest, SettleMessagesRequest, StorageQueueConfig,
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

fn save_and_lease(provider: &MemoryProvider, queue_id: &str, ttl: Duration) -> LeasedMessage {
    provider
        .save_message(SaveMessageRequest {
            queue_id: queue_id.to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: queue_id.to_string(),
            batch_size: 1,
            lease_ttl: Some(ttl),
        })
        .expect("lease_messages")
        .messages
        .remove(0)
}

#[test]
fn renewing_extends_the_lease_to_now_plus_the_extension() {
    let provider = provider_with_queue("orders");
    let leased = save_and_lease(&provider, "orders", Duration::from_secs(30));

    let before = SystemTime::now();
    let renewed = provider
        .renew_lease(RenewLeaseRequest {
            queue_id: "orders".to_string(),
            message_id: leased.message_id.clone(),
            lease_token: leased.lease_token.clone(),
            extension: Duration::from_secs(600),
        })
        .expect("renew_lease");
    let after = SystemTime::now();

    // §3.3.3: now + extension, not current expiry + extension.
    assert!(renewed.lease_expires_at >= before + Duration::from_secs(600));
    assert!(renewed.lease_expires_at <= after + Duration::from_secs(600));
}

fn renew(
    provider: &MemoryProvider,
    leased: &LeasedMessage,
    token: &str,
) -> Result<Duration, Error> {
    provider
        .renew_lease(RenewLeaseRequest {
            queue_id: "orders".to_string(),
            message_id: leased.message_id.clone(),
            lease_token: token.to_string(),
            extension: Duration::from_secs(600),
        })
        .map(|_| Duration::ZERO)
}

#[test]
fn renewing_with_a_rotated_token_fails_lease_lost_and_changes_nothing() {
    let provider = provider_with_queue("orders");
    let first = save_and_lease(&provider, "orders", Duration::from_secs(1));
    std::thread::sleep(Duration::from_millis(1_100));
    // Redelivery rotates the token; `first.lease_token` is now stale.
    let second = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: Some(Duration::from_secs(30)),
        })
        .expect("redelivery")
        .messages
        .remove(0);

    let error = renew(&provider, &first, &first.lease_token).expect_err("stale token");

    assert_eq!(error, Error::lease_lost());
    // Unchanged: the current token still renews (§3.3.3 does not rotate it).
    renew(&provider, &second, &second.lease_token).expect("current token still renews");
}

#[test]
fn renewing_an_expired_lease_fails_lease_lost() {
    let provider = provider_with_queue("orders");
    let leased = save_and_lease(&provider, "orders", Duration::from_secs(1));

    std::thread::sleep(Duration::from_millis(1_100));

    let error = renew(&provider, &leased, &leased.lease_token).expect_err("expired lease");
    assert_eq!(error, Error::lease_lost());
}

#[test]
fn settling_with_a_rotated_token_reports_lease_lost_and_changes_nothing() {
    let provider = provider_with_queue("orders");
    let first = save_and_lease(&provider, "orders", Duration::from_secs(1));
    std::thread::sleep(Duration::from_millis(1_100));
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: Some(Duration::from_secs(30)),
        })
        .expect("redelivery");

    let response = provider
        .settle_messages(SettleMessagesRequest {
            settlements: vec![StorageSettlement {
                queue_id: "orders".to_string(),
                message_id: first.message_id.clone(),
                lease_token: first.lease_token.clone(),
                outcome: Outcome::AckDelete,
                retry_delay: Duration::ZERO,
                dead_letter_reason: String::new(),
            }],
        })
        .expect("settle_messages");

    assert_eq!(response.results[0].code, code::FAILED_PRECONDITION);
    assert_eq!(response.results[0].error_reason, reason::LEASE_LOST);
    // Untouched: the ack did not delete it.
    let stats = provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "orders".to_string(),
        })
        .expect("get_queue_stats");
    assert_eq!(stats.leased_count, 1);
}
