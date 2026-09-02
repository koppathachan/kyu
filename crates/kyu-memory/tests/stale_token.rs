//! Protocol specification §8.1 requirement 4 — stale tokens (§3.3).

use std::time::{Duration, SystemTime};

use kyu_conformance::spi::{
    EnsureQueueRequest, LeaseMessagesRequest, LeasedMessage, RenewLeaseRequest, SaveMessageRequest,
    StorageQueueConfig,
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
