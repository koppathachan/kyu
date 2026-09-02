//! Unit tests for behaviour the §8.1 conformance suite does not reach: the
//! design D9 decisions, and the §4 limits of design D8.

use std::collections::HashMap;
use std::time::Duration;

use kyu_conformance::spi::*;
use kyu_memory::MemoryProvider;

const TTL: Duration = Duration::from_secs(30);
const OVER_TWELVE_HOURS: Duration = Duration::from_secs(13 * 60 * 60);

fn cfg(queue_id: &str) -> StorageQueueConfig {
    StorageQueueConfig {
        queue_id: queue_id.to_string(),
        default_lease_ttl: TTL,
        max_deliveries: 5,
        retry_backoff_base: Duration::from_millis(10),
        retry_backoff_max: Duration::from_secs(1),
        dedup_window: Duration::from_secs(30),
        dead_letter_enabled: true,
    }
}

fn ensure(p: &MemoryProvider, config: StorageQueueConfig) -> EnsureQueueResponse {
    p.ensure_queue(EnsureQueueRequest { config })
        .expect("ensure_queue failed")
}

fn save(p: &MemoryProvider, queue_id: &str) -> String {
    p.save_message(SaveMessageRequest {
        queue_id: queue_id.to_string(),
        payload: b"x".to_vec(),
        ..Default::default()
    })
    .expect("save_message failed")
    .message_id
}

fn lease_one(p: &MemoryProvider, queue_id: &str) -> LeasedMessage {
    p.lease_messages(LeaseMessagesRequest {
        queue_id: queue_id.to_string(),
        batch_size: 1,
        lease_ttl: Some(TTL),
    })
    .expect("lease_messages failed")
    .messages
    .pop()
    .expect("a message must be leasable")
}

fn expect_validation_failed<T: std::fmt::Debug>(r: Result<T>, what: &str) {
    let err = match r {
        Err(e) => e,
        Ok(v) => panic!("{what} must be rejected, got {v:?}"),
    };
    assert_eq!(
        (err.code, err.reason.as_str()),
        (code::INVALID_ARGUMENT, reason::VALIDATION_FAILED),
        "{what}"
    );
}

// --- design D9: EnsureQueue never conflicts -------------------------------

#[test]
fn ensure_queue_returns_the_stored_config_on_mismatch() {
    let p = MemoryProvider::new();
    let first = ensure(&p, cfg("q"));
    assert!(first.created);

    let mut different = cfg("q");
    different.max_deliveries = 3;
    different.dead_letter_enabled = false;
    let second = ensure(&p, different);

    assert!(!second.created, "an existing queue is not created again");
    assert_eq!(
        second.config,
        cfg("q"),
        "the response carries the STORED config, not the requested one"
    );
}

// --- design D9: ListQueues paging -----------------------------------------

#[test]
fn list_queues_pages_in_queue_id_order() {
    let p = MemoryProvider::new();
    for id in ["d", "b", "a", "c"] {
        ensure(&p, cfg(id));
    }

    let all = p
        .list_queues(ListQueuesRequest {
            page_size: 0,
            page_token: String::new(),
        })
        .expect("list_queues failed");
    let ids: Vec<&str> = all.queues.iter().map(|c| c.queue_id.as_str()).collect();
    assert_eq!(
        ids,
        ["a", "b", "c", "d"],
        "page_size = 0 returns all, ordered"
    );
    assert!(all.next_page_token.is_empty());

    let first = p
        .list_queues(ListQueuesRequest {
            page_size: 2,
            page_token: String::new(),
        })
        .expect("list_queues failed");
    let ids: Vec<&str> = first.queues.iter().map(|c| c.queue_id.as_str()).collect();
    assert_eq!(ids, ["a", "b"]);
    assert_eq!(
        first.next_page_token, "b",
        "the token is the last id returned"
    );

    let second = p
        .list_queues(ListQueuesRequest {
            page_size: 2,
            page_token: first.next_page_token,
        })
        .expect("list_queues failed");
    let ids: Vec<&str> = second.queues.iter().map(|c| c.queue_id.as_str()).collect();
    assert_eq!(ids, ["c", "d"], "the next page continues, skipping nothing");
    assert!(
        second.next_page_token.is_empty(),
        "the last page reports no token"
    );
}

// --- design D9: DeleteQueue counts every state ----------------------------

#[test]
fn delete_queue_counts_messages_in_every_state() {
    let p = MemoryProvider::new();
    ensure(&p, cfg("mixed"));
    save(&p, "mixed"); // stays READY
    save(&p, "mixed"); // becomes LEASED
    save(&p, "mixed"); // becomes DEAD_LETTER

    let leased = lease_one(&p, "mixed");
    let doomed = lease_one(&p, "mixed");
    let results = p
        .settle_messages(SettleMessagesRequest {
            settlements: vec![StorageSettlement {
                queue_id: "mixed".into(),
                message_id: doomed.message_id.clone(),
                lease_token: doomed.lease_token.clone(),
                outcome: Outcome::DeadLetter,
                retry_delay: Duration::ZERO,
                dead_letter_reason: "poison".into(),
            }],
        })
        .expect("settle_messages failed")
        .results;
    assert_eq!(results[0].code, code::OK);

    let stats = p
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "mixed".into(),
        })
        .expect("get_queue_stats failed");
    assert_eq!(
        stats,
        GetQueueStatsResponse {
            ready_count: 1,
            leased_count: 1,
            dead_letter_count: 1
        }
    );
    drop(leased);

    let deleted = p
        .delete_queue(DeleteQueueRequest {
            queue_id: "mixed".into(),
        })
        .expect("delete_queue failed");
    assert_eq!(
        deleted.purged_message_count, 3,
        "the purge count covers ready, leased and dead-lettered alike"
    );
}

// --- design D9: per-item settlement failures ------------------------------

#[test]
fn settling_an_unknown_message_is_a_per_item_failure() {
    let p = MemoryProvider::new();
    ensure(&p, cfg("q"));
    save(&p, "q");
    let live = lease_one(&p, "q");

    let results = p
        .settle_messages(SettleMessagesRequest {
            settlements: vec![
                StorageSettlement {
                    queue_id: "q".into(),
                    message_id: "no-such-message".into(),
                    lease_token: "whatever".into(),
                    outcome: Outcome::AckDelete,
                    retry_delay: Duration::ZERO,
                    dead_letter_reason: String::new(),
                },
                StorageSettlement {
                    queue_id: "q".into(),
                    message_id: live.message_id.clone(),
                    lease_token: live.lease_token.clone(),
                    outcome: Outcome::AckDelete,
                    retry_delay: Duration::ZERO,
                    dead_letter_reason: String::new(),
                },
            ],
        })
        .expect("an unknown message must not fail the RPC")
        .results;

    assert_eq!(
        results.len(),
        2,
        "one result per input item, in request order"
    );
    assert_eq!(
        (results[0].code, results[0].error_reason.as_str()),
        (code::NOT_FOUND, reason::MESSAGE_NOT_FOUND)
    );
    assert_eq!(
        results[1].code,
        code::OK,
        "the rest of the batch is still applied"
    );
}

#[test]
fn an_empty_settlement_batch_is_rejected() {
    let p = MemoryProvider::new();
    expect_validation_failed(
        p.settle_messages(SettleMessagesRequest {
            settlements: vec![],
        }),
        "an empty settlement batch",
    );
}

// --- §4 limits (design D8) -------------------------------------------------

#[test]
fn oversized_requests_are_rejected() {
    let p = MemoryProvider::new();
    ensure(&p, cfg("q"));

    expect_validation_failed(
        p.save_message(SaveMessageRequest {
            queue_id: "q".into(),
            payload: vec![0u8; 1024 * 1024 + 1],
            ..Default::default()
        }),
        "a payload over 1 MiB",
    );

    let too_many: HashMap<String, String> =
        (0..17).map(|i| (format!("k{i}"), "v".into())).collect();
    expect_validation_failed(
        p.save_message(SaveMessageRequest {
            queue_id: "q".into(),
            attributes: too_many,
            ..Default::default()
        }),
        "more than 16 attributes",
    );

    let too_big: HashMap<String, String> = HashMap::from([("k".to_string(), "v".repeat(4 * 1024))]);
    expect_validation_failed(
        p.save_message(SaveMessageRequest {
            queue_id: "q".into(),
            attributes: too_big,
            ..Default::default()
        }),
        "attributes over 4 KiB",
    );

    expect_validation_failed(
        p.save_message(SaveMessageRequest {
            queue_id: "q".into(),
            idempotency_key: "k".repeat(257),
            ..Default::default()
        }),
        "an idempotency key over 256 bytes",
    );

    expect_validation_failed(
        p.save_message(SaveMessageRequest {
            queue_id: String::new(),
            ..Default::default()
        }),
        "an empty queue_id",
    );
    expect_validation_failed(
        p.save_message(SaveMessageRequest {
            queue_id: "q".repeat(257),
            ..Default::default()
        }),
        "a queue_id over 256 bytes",
    );

    let mut zero_limit = cfg("bad");
    zero_limit.max_deliveries = 0;
    expect_validation_failed(
        p.ensure_queue(EnsureQueueRequest { config: zero_limit }),
        "max_deliveries below 1",
    );

    expect_validation_failed(
        p.lease_messages(LeaseMessagesRequest {
            queue_id: "q".into(),
            batch_size: 101,
            lease_ttl: Some(TTL),
        }),
        "a batch_size over 100",
    );
    expect_validation_failed(
        p.lease_messages(LeaseMessagesRequest {
            queue_id: "q".into(),
            batch_size: 1,
            lease_ttl: Some(OVER_TWELVE_HOURS),
        }),
        "a lease_ttl over 12 hours",
    );

    expect_validation_failed(
        p.renew_lease(RenewLeaseRequest {
            queue_id: "q".into(),
            message_id: "m".into(),
            lease_token: "t".into(),
            extension: OVER_TWELVE_HOURS,
        }),
        "an extension over 12 hours",
    );

    expect_validation_failed(
        p.settle_messages(SettleMessagesRequest {
            settlements: vec![StorageSettlement {
                queue_id: "q".into(),
                message_id: "m".into(),
                lease_token: "t".into(),
                outcome: Outcome::Retry,
                retry_delay: OVER_TWELVE_HOURS,
                dead_letter_reason: String::new(),
            }],
        }),
        "a retry_delay over 12 hours",
    );

    let thousand_and_one: Vec<StorageSettlement> = (0..1001)
        .map(|i| StorageSettlement {
            queue_id: "q".into(),
            message_id: format!("m{i}"),
            lease_token: "t".into(),
            outcome: Outcome::AckDelete,
            retry_delay: Duration::ZERO,
            dead_letter_reason: String::new(),
        })
        .collect();
    expect_validation_failed(
        p.settle_messages(SettleMessagesRequest {
            settlements: thousand_and_one,
        }),
        "more than 1000 settlements",
    );
}

/// Design D8: the §4 minimum of 1 s on `lease_ttl` and `extension` is
/// deliberately not enforced.
#[test]
fn a_sub_second_lease_ttl_is_accepted() {
    let p = MemoryProvider::new();
    ensure(&p, cfg("short"));
    save(&p, "short");

    let leased = p
        .lease_messages(LeaseMessagesRequest {
            queue_id: "short".into(),
            batch_size: 1,
            lease_ttl: Some(Duration::from_millis(300)),
        })
        .expect("a 300 ms lease TTL must be accepted")
        .messages;
    assert_eq!(leased.len(), 1);

    p.renew_lease(RenewLeaseRequest {
        queue_id: "short".into(),
        message_id: leased[0].message_id.clone(),
        lease_token: leased[0].lease_token.clone(),
        extension: Duration::from_millis(300),
    })
    .expect("a 300 ms extension must be accepted");
}

// --- unknown queues --------------------------------------------------------

#[test]
fn queue_scoped_requests_reject_an_unknown_queue() {
    let p = MemoryProvider::new();
    for err in [
        p.save_message(SaveMessageRequest {
            queue_id: "ghost".into(),
            ..Default::default()
        })
        .map(|_| ())
        .unwrap_err(),
        p.lease_messages(LeaseMessagesRequest {
            queue_id: "ghost".into(),
            batch_size: 1,
            lease_ttl: Some(TTL),
        })
        .map(|_| ())
        .unwrap_err(),
        p.renew_lease(RenewLeaseRequest {
            queue_id: "ghost".into(),
            message_id: "m".into(),
            lease_token: "t".into(),
            extension: TTL,
        })
        .map(|_| ())
        .unwrap_err(),
        p.get_queue_config(GetQueueConfigRequest {
            queue_id: "ghost".into(),
        })
        .map(|_| ())
        .unwrap_err(),
        p.get_queue_stats(GetQueueStatsRequest {
            queue_id: "ghost".into(),
        })
        .map(|_| ())
        .unwrap_err(),
        p.delete_queue(DeleteQueueRequest {
            queue_id: "ghost".into(),
        })
        .map(|_| ())
        .unwrap_err(),
    ] {
        assert_eq!(
            (err.code, err.reason.as_str()),
            (code::NOT_FOUND, reason::QUEUE_NOT_FOUND)
        );
    }
}
