//! In-memory Kyu v1 storage provider.

use kyu_conformance::spi::{
    CONTRACT_VERSION, GetProviderInfoRequest, GetProviderInfoResponse, Result,
};

/// An in-memory implementation of the Kyu v1 storage provider SPI (§6).
#[derive(Debug, Default)]
pub struct MemoryProvider;

impl MemoryProvider {
    pub fn new() -> Self {
        Self
    }

    pub fn get_provider_info(
        &self,
        _req: GetProviderInfoRequest,
    ) -> Result<GetProviderInfoResponse> {
        Ok(GetProviderInfoResponse {
            provider_name: "memory".to_string(),
            provider_version: env!("CARGO_PKG_VERSION").to_string(),
            contract_version: CONTRACT_VERSION.to_string(),
        })
    }
}
