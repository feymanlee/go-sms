# Live Provider Verification

Live tests are excluded from ordinary CI by the `integration` build tag. Each
enabled test submits exactly one SMS through its Provider; do not run these
tests unless the recipient and template are approved for a real send.

Provide these common variables to the test process:

- `GO_SMS_TEST_RECIPIENT`
- `GO_SMS_TEST_PARAM_NAME`
- `GO_SMS_TEST_PARAM_VALUE`

Provide the variables for every Provider being verified:

| Provider | Variables |
|---|---|
| Tencent Cloud | `TENCENT_SECRET_ID`, `TENCENT_SECRET_KEY`, `TENCENT_SMS_APP_ID`, `TENCENT_REGION`, `TENCENT_TEMPLATE_ID`, `TENCENT_SIGNATURE_REF` |
| Alibaba Cloud | `ALIYUN_ACCESS_KEY_ID`, `ALIYUN_ACCESS_KEY_SECRET`, `ALIYUN_REGION`, `ALIYUN_TEMPLATE_ID`, `ALIYUN_SIGNATURE_REF` |
| UCloud | `UCLOUD_PUBLIC_KEY`, `UCLOUD_PRIVATE_KEY`, `UCLOUD_PROJECT_ID`, `UCLOUD_REGION`, `UCLOUD_TEMPLATE_ID`, `UCLOUD_SIGNATURE_REF` |
| Qiniu Cloud | `QINIU_ACCESS_KEY`, `QINIU_SECRET_KEY`, `QINIU_TEMPLATE_ID`, `QINIU_SIGNATURE_REF` |
| Yunpian | `YUNPIAN_API_KEY`, `YUNPIAN_TEMPLATE_ID` |

Run all five checks with:

```sh
go test -tags=integration ./provider/... -run TestIntegrationSend -count=1 -v
```

The tests read credentials only from the test process environment. Go's
standard `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` discovery remains active
unless a different client transport is injected.

## Output and release records

On acceptance, live test logs include the UTC date, Provider, and the complete
`MessageID` and `RequestID` returned by that Provider. This complete identifier
output is permitted only as ephemeral local verification output. Never commit,
attach, or otherwise preserve the raw test output in the repository.

For release notes, manually redact each `MessageID` and `RequestID` before
recording the UTC date, Provider, and verification result. A release-note entry
must never contain a complete identifier.

Credentials, complete Recipient values, template parameter values, complete
request bodies, shell-history exports, and test output containing those values
must never be logged or committed. Before committing release notes, inspect the
diff and repository search results for accidental sensitive values.
