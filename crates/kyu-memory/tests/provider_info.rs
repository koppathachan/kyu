//! Protocol specification §8.1 requirement 13 — provider info.

use kyu_conformance::spi::{CONTRACT_VERSION, GetProviderInfoRequest};
use kyu_memory::MemoryProvider;

#[test]
fn reports_the_v1_contract_version() {
    let provider = MemoryProvider::new();

    let info = provider
        .get_provider_info(GetProviderInfoRequest {})
        .expect("get_provider_info");

    assert_eq!(info.contract_version, CONTRACT_VERSION);
}
