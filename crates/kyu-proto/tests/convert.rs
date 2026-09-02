//! Conversions between the generated `kyu.storage.v1` types and the in-process
//! SPI types. §1: both bindings are the same contract.

use std::time::Duration;

use kyu_proto::convert::{duration_from_proto, duration_to_proto};

#[test]
fn a_duration_round_trips_through_its_proto_form() {
    let duration = Duration::from_millis(1_500);

    let restored = duration_from_proto(Some(duration_to_proto(duration)));

    assert_eq!(restored, Some(duration));
}
