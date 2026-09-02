//! Protocol specification §8.1.8 — concurrency.

use std::collections::HashSet;
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use kyu_conformance::spi::{
    EnsureQueueRequest, GetQueueStatsRequest, LeaseMessagesRequest, Outcome, SaveMessageRequest,
    SettleMessagesRequest, StorageQueueConfig, StorageSettlement, code,
};
use kyu_memory::MemoryProvider;

const MESSAGES: usize = 200;
const LEASERS: usize = 8;

fn provider_with_saved_messages() -> Arc<MemoryProvider> {
    let provider = Arc::new(MemoryProvider::new());
    provider
        .ensure_queue(EnsureQueueRequest {
            config: StorageQueueConfig {
                queue_id: "orders".to_string(),
                // Long enough that no lease expires during the test.
                default_lease_ttl: Duration::from_secs(600),
                max_deliveries: 5,
                retry_backoff_base: Duration::from_secs(1),
                retry_backoff_max: Duration::from_secs(60),
                dedup_window: Duration::from_secs(300),
                dead_letter_enabled: true,
            },
        })
        .expect("ensure_queue");
    for _ in 0..MESSAGES {
        provider
            .save_message(SaveMessageRequest {
                queue_id: "orders".to_string(),
                ..SaveMessageRequest::default()
            })
            .expect("save_message");
    }
    provider
}

/// Every leaser drains the queue in batches until it comes back empty.
fn drain(provider: &MemoryProvider) -> Vec<String> {
    let mut leased = Vec::new();
    loop {
        let batch = provider
            .lease_messages(LeaseMessagesRequest {
                queue_id: "orders".to_string(),
                batch_size: 10,
                lease_ttl: None,
            })
            .expect("lease_messages")
            .messages;
        if batch.is_empty() {
            return leased;
        }
        leased.extend(batch.into_iter().map(|message| message.message_id));
    }
}

#[test]
fn concurrent_leasers_never_receive_the_same_message() {
    let provider = provider_with_saved_messages();

    let leasers: Vec<_> = (0..LEASERS)
        .map(|_| {
            let provider = Arc::clone(&provider);
            thread::spawn(move || drain(&provider))
        })
        .collect();
    let delivered: Vec<String> = leasers
        .into_iter()
        .flat_map(|leaser| leaser.join().expect("leaser thread"))
        .collect();

    assert!(!delivered.is_empty(), "the leasers delivered nothing");
    let distinct: HashSet<&String> = delivered.iter().collect();
    assert_eq!(
        distinct.len(),
        delivered.len(),
        "a message was delivered twice while its lease was live"
    );
}

#[test]
fn every_saved_message_is_delivered_at_least_once() {
    let provider = provider_with_saved_messages();

    let leasers: Vec<_> = (0..LEASERS)
        .map(|_| {
            let provider = Arc::clone(&provider);
            thread::spawn(move || drain(&provider))
        })
        .collect();
    let delivered: HashSet<String> = leasers
        .into_iter()
        .flat_map(|leaser| leaser.join().expect("leaser thread"))
        .collect();

    // A leaser stops only on an empty batch, and leases outlive the test, so
    // an undelivered message would mean one was lost, not merely late.
    assert_eq!(delivered.len(), MESSAGES);
}

/// Every leaser acks what it leases, and reports how many acks the provider
/// accepted.
fn drain_and_ack(provider: &MemoryProvider) -> usize {
    let mut acked = 0;
    loop {
        let batch = provider
            .lease_messages(LeaseMessagesRequest {
                queue_id: "orders".to_string(),
                batch_size: 10,
                lease_ttl: None,
            })
            .expect("lease_messages")
            .messages;
        if batch.is_empty() {
            return acked;
        }
        let settlements = batch
            .into_iter()
            .map(|message| StorageSettlement {
                queue_id: "orders".to_string(),
                message_id: message.message_id,
                lease_token: message.lease_token,
                outcome: Outcome::AckDelete,
                retry_delay: Duration::ZERO,
                dead_letter_reason: String::new(),
            })
            .collect();
        acked += provider
            .settle_messages(SettleMessagesRequest { settlements })
            .expect("settle_messages")
            .results
            .iter()
            .filter(|result| result.code == code::OK)
            .count();
    }
}

#[test]
fn the_number_acked_equals_the_number_saved() {
    let provider = provider_with_saved_messages();

    let leasers: Vec<_> = (0..LEASERS)
        .map(|_| {
            let provider = Arc::clone(&provider);
            thread::spawn(move || drain_and_ack(&provider))
        })
        .collect();
    let acked: usize = leasers
        .into_iter()
        .map(|leaser| leaser.join().expect("leaser thread"))
        .sum();

    assert_eq!(acked, MESSAGES);
    let stats = provider
        .get_queue_stats(GetQueueStatsRequest {
            queue_id: "orders".to_string(),
        })
        .expect("get_queue_stats");
    assert_eq!(
        (
            stats.ready_count,
            stats.leased_count,
            stats.dead_letter_count
        ),
        (0, 0, 0),
        "acking removes the message"
    );
}
