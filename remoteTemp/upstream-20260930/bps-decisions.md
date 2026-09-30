# BPS upstream decisions (2026-09-30)

## Sources

- Baseline before integration: `dcda8558019ce7e8a855d61f6251e357472c5059`
- Official `old-origin/main`: `a60a29549f488a854966aaec9541abbe006cac22`
- Ranxi `ranxi/production`: `7124114c22c7cb786a62d5e3ee64713ca87ebdfc`

## Directly absorbed

- BPS protocol compatibility in `backend/internal/service/basispoints`: response/input normalization, attachment protocol handling, tool catalog and repair, stream failure normalization, typed upstream failures, and usage merging.
- BPS image attachment diagnostics and safe error classification. Private upstream response content is excluded from client errors and persisted diagnostics.
- BPS in-band `401/403/429` handling. In-band `429` cools only the BPS route and never replays a generation after output was accepted; real HTTP `5xx` retains the existing account-health path.
- Existing local rolling image digest, native attachment capacity limits, random/fixed proxy selection, TLS profile propagation, usage accounting, and same-egress attachment/generation pinning remain enabled.

## Adapted

- Typed local egress failures survive the Responses and native-attachment `Forward` error wrappers as allowlisted sentinels, without exposing raw transport errors; the scheduler excludes them from account-health penalties and success recovery. BPS semantic failures are classified by `basispoints.UpstreamFailure` rather than matching arbitrary error text.
- Initial HTTP admission keeps the local bounded retry behavior and excludes continuation requests, committed responses, cancellation, exhausted budget, and the previous account.
- WebSocket/SSE acceleration is opt-in and limited to ordinary OAuth requests without continuation, passthrough, or plugin paths; typed handshake failures are preserved.

## Kept local

- Local random proxy runtime selection, TLS fingerprint context, rolling image digest, native image attachment chain, and existing usage/billing behavior take precedence over the upstream image-policy and Mihomo-specific paths.
- Existing Claude/panel RPM logic remains; the new upstream OpenAI RPM admission/scheduler chain is not used.

## Deferred

- Mihomo warm/recovery/acquisition and the associated image-policy, image-capacity, compaction, and generated-image API chain are deferred pending a complete dependency and runtime review.
- Deferred files include `openai_excel_bps_image_policy.go`, `openai_excel_bps_images.go`, `openai_excel_bps_recovery.go`, `openai_excel_bps_warm.go`, and their tests.
- The temporary merge tree still contains Ranxi `openai_oauth_reauth.go`, which imports the deferred `internal/mihomo` package. The service package cannot compile until that dependency is either fully adopted or the reauth path is deferred/ adapted by the integration owner.

## Verification evidence

- Passed: `go test -count=1 ./internal/service/basispoints`.
- Passed: no Go conflict markers under `backend` (`rg -n '^<<<<<<<|^=======|^>>>>>>>$' backend --glob '*.go'`).
- Passed: `git diff --check` for the BPS files touched in this pass.
- Added but not yet executed: `TestExcelBPSLocalEgressFailureReachesAccountScheduler` covers Responses and native-attachment failures, safe error text, and scheduler non-penalty for both random-proxy sentinels.
- Blocked: `go test -count=1 ./internal/service -run 'TestExcelBPSLocalEgressFailureReachesAccountScheduler|TestExcelBPSNativeUploadFailureStopsWithoutQuotaWrite|TestOpenAIAccountSchedulingIgnoresLocalEgressAndBPSInBandAuth'` with `GOARCH=amd64` stops at syntax errors in `account_test_service.go` and `openai_account_runtime_block_fastpath.go` from the still-unresolved merge. An earlier attempt also encountered missing `internal/mihomo` from the deferred Ranxi chain. The host Go default is `GOARCH=386`, which separately overflows constants in `internal/pkg/openaiauth`.
