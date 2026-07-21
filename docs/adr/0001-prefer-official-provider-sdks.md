# Prefer official Provider SDKs with selective HTTP fallbacks

Provider implementations use maintained official Go SDKs by default so that authentication, request signing, and API evolution remain aligned with each Provider. A Provider may use a direct HTTP implementation when its official SDK is unavailable, stale, or cannot support the required SMS API; vendor SDK types remain behind the common Provider boundary so this choice can change without breaking callers.
