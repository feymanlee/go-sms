# SMS Sending

This context defines the shared language for sending SMS messages through external service vendors.

## Language

**Provider**:
An external SMS service vendor selected explicitly by the caller for one send attempt.
_Avoid_: Channel, gateway, platform

**Send Attempt**:
One non-retried request submitted to exactly one Provider. Version 1 does not route, retry, or fail over automatically.
_Avoid_: Dispatch, delivery

**Send Failure**:
An unsuccessful or indeterminate Send Attempt after the request has crossed the Provider seam. Validation, cancellation, or implementation errors before submission are not Send Failures. A Send Failure exposes only safe structured Provider diagnostics.
_Avoid_: Provider error, send error

**Failure Category**:
A library-owned classification of a Send Failure: Authentication, Rate Limited, Rejected, Unavailable, or Unknown Outcome. Provider-native codes are mapped to a Failure Category by the selected Provider adapter.
_Avoid_: Error kind, retryable flag

**Submission**:
Evidence that a Provider accepted a Send Attempt, including common identifiers and optional Provider Metadata. It is not evidence that the SMS reached the Recipient.
_Avoid_: Success, delivery, receipt

**Provider Metadata**:
Provider-specific string values retained with a Submission when they have no stable cross-Provider meaning.
_Avoid_: Raw response, SDK response

**Template Message**:
An SMS message identified by the selected Provider's native template identifier and populated with template parameters. Callers resolve business templates to Provider templates before creating it; it is the only message form supported in version 1.
_Avoid_: Raw message, text message

**Template Parameter**:
A named value with a stable position in a Template Message. Providers may address it by name or by position.
_Avoid_: Template variable, template data

**Signature Reference**:
A Provider-native value selecting an approved SMS signature; depending on the Provider, it may be a signature name, content, or identifier. A Provider may define a default Signature Reference, which a Send Attempt may override.
_Avoid_: API signature, request signature, SMS signature

**Recipient**:
The single E.164 phone number targeted by a Send Attempt. Sending to multiple phone numbers is outside version 1.
_Avoid_: Recipients, audience
