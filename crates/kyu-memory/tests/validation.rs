//! Protocol specification §4 — limits and validation.

use std::collections::HashMap;
use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, Error, LeaseMessagesRequest, Outcome, RenewLeaseRequest,
    SaveMessageRequest, SettleMessagesRequest, StorageProvider, StorageQueueConfig,
    StorageSettlement, code, reason,
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

#[test]
fn a_payload_over_one_mebibyte_fails_validation() {
    let provider = provider_with_queue("orders");

    let error = provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            payload: vec![0u8; 1024 * 1024 + 1],
            ..SaveMessageRequest::default()
        })
        .expect_err("over the 1 MiB limit");

    assert_eq!(error, Error::validation_failed());
    // 1 MiB exactly is the limit, not one past it.
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            payload: vec![0u8; 1024 * 1024],
            ..SaveMessageRequest::default()
        })
        .expect("1 MiB is legal");
}

#[test]
fn oversized_attributes_fail_validation() {
    let provider = provider_with_queue("orders");

    let too_many: HashMap<String, String> = (0..17)
        .map(|i| (format!("k{i}"), "v".to_string()))
        .collect();
    assert_eq!(
        save_attributes(&provider, too_many).expect_err("17 entries"),
        Error::validation_failed()
    );

    let too_big: HashMap<String, String> = HashMap::from([("k".to_string(), "v".repeat(4 * 1024))]);
    assert_eq!(
        save_attributes(&provider, too_big).expect_err("over 4 KiB serialised"),
        Error::validation_failed()
    );

    let at_the_limit: HashMap<String, String> = (0..16)
        .map(|i| (format!("k{i}"), "v".to_string()))
        .collect();
    save_attributes(&provider, at_the_limit).expect("16 small entries are legal");
}

fn save_attributes(
    provider: &MemoryProvider,
    attributes: HashMap<String, String>,
) -> Result<(), Error> {
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            attributes,
            ..SaveMessageRequest::default()
        })
        .map(|_| ())
}

#[test]
fn an_oversized_idempotency_key_fails_validation() {
    let provider = provider_with_queue("orders");

    let error = provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            idempotency_key: "k".repeat(257),
            ..SaveMessageRequest::default()
        })
        .expect_err("over the 256 byte limit");

    assert_eq!(error, Error::validation_failed());
    // 256 bytes is the limit, not one past it.
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            idempotency_key: "k".repeat(256),
            ..SaveMessageRequest::default()
        })
        .expect("256 bytes is legal");
}

#[test]
fn a_batch_size_of_zero_leases_at_most_one_message() {
    let provider = provider_with_queue("orders");
    for _ in 0..2 {
        provider
            .save_message(SaveMessageRequest {
                queue_id: "orders".to_string(),
                ..SaveMessageRequest::default()
            })
            .expect("save_message");
    }

    let leased = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 0,
            lease_ttl: None,
        })
        .expect("lease_messages");

    // §4: batch_size 0 means 1, not "none".
    assert_eq!(leased.messages.len(), 1);
}

#[test]
fn a_batch_size_over_one_hundred_fails_validation() {
    let provider = provider_with_queue("orders");

    let error = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 101,
            lease_ttl: None,
        })
        .expect_err("over the 100 message limit");

    assert_eq!(error, Error::validation_failed());
    // 100 is the limit, not one past it.
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 100,
            lease_ttl: None,
        })
        .expect("100 is legal");
}

#[test]
fn a_lease_ttl_over_twelve_hours_fails_validation() {
    let provider = provider_with_queue("orders");

    let error = lease_with_ttl(
        &provider,
        Duration::from_secs(12 * 60 * 60) + Duration::from_secs(1),
    )
    .expect_err("over the 12 hour limit");
    assert_eq!(error, Error::validation_failed());

    // 12 hours is the limit, not one past it.
    lease_with_ttl(&provider, Duration::from_secs(12 * 60 * 60)).expect("12 hours is legal");
    // §4 makes the MUST an *upper* bound; the 1 second minimum in its table is
    // a default an implementation MAY differ from, so a sub-second lease is
    // accepted here. Expiry scenarios depend on it.
    lease_with_ttl(&provider, Duration::from_millis(300)).expect("a sub-second lease is legal");
}

fn lease_with_ttl(provider: &MemoryProvider, lease_ttl: Duration) -> Result<(), Error> {
    provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: Some(lease_ttl),
        })
        .map(|_| ())
}

#[test]
fn a_renew_extension_outside_one_second_to_twelve_hours_fails_validation() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    let leased = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: None,
        })
        .expect("lease_messages")
        .messages
        .remove(0);

    let renew = |extension| {
        provider
            .renew_lease(RenewLeaseRequest {
                queue_id: "orders".to_string(),
                message_id: leased.message_id.clone(),
                lease_token: leased.lease_token.clone(),
                extension,
            })
            .map(|_| ())
    };

    for extension in [
        Duration::from_millis(999),
        Duration::from_secs(12 * 60 * 60) + Duration::from_secs(1),
    ] {
        let error = renew(extension).expect_err("extension out of range");
        assert_eq!(error, Error::validation_failed(), "{extension:?}");
    }

    // Both ends of the range are legal, not one past them.
    for extension in [Duration::from_secs(1), Duration::from_secs(12 * 60 * 60)] {
        renew(extension).expect("at the boundary");
    }
}

#[test]
fn an_empty_settlement_list_fails_validation() {
    let provider = provider_with_queue("orders");

    let error = provider
        .settle_messages(SettleMessagesRequest {
            settlements: Vec::new(),
        })
        .expect_err("§4: empty ⇒ VALIDATION_FAILED");

    assert_eq!(error, Error::validation_failed());
}

#[test]
fn more_than_one_thousand_settlements_fail_validation() {
    let provider = provider_with_queue("orders");

    let settlements = |count| {
        (0..count)
            .map(|i| StorageSettlement {
                queue_id: "orders".to_string(),
                message_id: format!("{i}"),
                lease_token: String::new(),
                outcome: Outcome::AckDelete,
                retry_delay: Duration::ZERO,
                dead_letter_reason: String::new(),
            })
            .collect()
    };

    let error = provider
        .settle_messages(SettleMessagesRequest {
            settlements: settlements(1001),
        })
        .expect_err("over the 1000 item limit");

    assert_eq!(error, Error::validation_failed());
    // 1000 is the limit, not one past it.
    provider
        .settle_messages(SettleMessagesRequest {
            settlements: settlements(1000),
        })
        .expect("1000 is legal");
}

#[test]
fn a_retry_delay_over_twelve_hours_fails_validation() {
    let provider = provider_with_queue("orders");
    provider
        .save_message(SaveMessageRequest {
            queue_id: "orders".to_string(),
            ..SaveMessageRequest::default()
        })
        .expect("save_message");
    let leased = provider
        .lease_messages(LeaseMessagesRequest {
            queue_id: "orders".to_string(),
            batch_size: 1,
            lease_ttl: None,
        })
        .expect("lease_messages")
        .messages
        .remove(0);

    let retry_after = |retry_delay| {
        provider
            .settle_messages(SettleMessagesRequest {
                settlements: vec![StorageSettlement {
                    queue_id: "orders".to_string(),
                    message_id: leased.message_id.clone(),
                    lease_token: leased.lease_token.clone(),
                    outcome: Outcome::Retry,
                    retry_delay,
                    dead_letter_reason: String::new(),
                }],
            })
            .expect("§7: an item-level problem does not fail the RPC")
            .results
            .remove(0)
    };

    let over_the_limit = retry_after(Duration::from_secs(12 * 60 * 60) + Duration::from_secs(1));
    assert_eq!(over_the_limit.code, code::INVALID_ARGUMENT);
    assert_eq!(over_the_limit.error_reason, reason::VALIDATION_FAILED);

    // 12 hours is the limit, not one past it, and the rejected settlement left
    // the lease alone.
    assert_eq!(
        retry_after(Duration::from_secs(12 * 60 * 60)).code,
        code::OK
    );
}
