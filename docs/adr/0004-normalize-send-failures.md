# Normalize Send Failures behind a sealed module

Send Attempt failures remain ordinary Go `error` return values, but structured inspection moves to an immutable, sealed `failure.Failure` implemented in a public `failure` package. Provider adapters construct failures through a Provider-bound semantic factory; native classification stays local to each adapter, while the failure module owns safe diagnostics, category validation, Context matching, and the rule that an explicit Provider decision wins over concurrent cancellation.

The former mutable `SendError`, category sentinels, Provider messages, native causes, and native error identity are removed before the first tagged release. Errors before the request crosses the Provider seam remain ordinary errors; after invocation, any result without a definitive Provider decision is `UnknownOutcome`, optionally retaining only validated Code and RequestID tokens.
