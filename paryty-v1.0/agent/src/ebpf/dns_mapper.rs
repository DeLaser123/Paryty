#![allow(dead_code)]

//! DNS Resolver Mapper
//!
//! Maps domain names to IP addresses by intercepting DNS queries.

use std::collections::HashMap;

/// DNS resolution entry.
#[derive(Debug, Clone)]
pub struct DnsEntry {
    pub domain: String,
    pub ips: Vec<String>,
    pub ttl: u32,
    pub last_seen: std::time::Instant,
}

/// DNS resolver mapper.
pub struct DnsMapper {
    cache: std::sync::Mutex<HashMap<String, DnsEntry>>,
}

impl DnsMapper {
    pub fn new() -> Self {
        Self { cache: std::sync::Mutex::new(HashMap::new()) }
    }

    /// Get cached DNS entry for a domain.
    pub fn resolve(&self, domain: &str) -> Option<DnsEntry> {
        self.cache.lock().unwrap().get(domain).cloned()
    }

    /// Update DNS cache with a new resolution.
    pub fn update(&self, domain: String, ips: Vec<String>, ttl: u32) {
        let mut cache = self.cache.lock().unwrap();
        cache.insert(
            domain.clone(),
            DnsEntry { domain, ips, ttl, last_seen: std::time::Instant::now() },
        );
    }
}

impl Default for DnsMapper {
    fn default() -> Self {
        Self::new()
    }
}
