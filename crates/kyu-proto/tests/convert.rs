//! Conversions between the generated `kyu.storage.v1` types and the in-process
//! SPI types. §1: both bindings are the same contract.

use std::time::{Duration, UNIX_EPOCH};

use kyu_conformance::spi::MessageState;
use kyu_proto::convert::{
    duration_from_proto, duration_to_proto, message_state_from_proto, message_state_to_proto,
    timestamp_from_proto, timestamp_to_proto,
};

#[test]
fn a_duration_round_trips_through_its_proto_form() {
    let duration = Duration::from_millis(1_500);

    let restored = duration_from_proto(Some(duration_to_proto(duration)));

    assert_eq!(restored, Some(duration));
}

#[test]
fn a_system_time_round_trips_through_a_proto_timestamp() {
    let instant = UNIX_EPOCH + Duration::new(1_700_000_000, 123_456_789);

    let restored = timestamp_from_proto(Some(timestamp_to_proto(instant)));

    assert_eq!(restored, Some(instant));
}

#[test]
fn an_absent_proto_duration_converts_to_none() {
    assert_eq!(duration_from_proto(None), None);
}

#[test]
fn message_state_round_trips_through_its_proto_enum() {
    for state in [
        MessageState::Unspecified,
        MessageState::Ready,
        MessageState::Leased,
        MessageState::DeadLetter,
    ] {
        assert_eq!(
            message_state_from_proto(message_state_to_proto(state)),
            state
        );
    }
}
