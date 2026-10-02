# BPS upstream decisions (2026-09-30)

## Sources

- Baseline before integration: `dcda8558019ce7e8a855d61f6251e357472c5059`
- Official `old-origin/main`: `a0f41f95a07ee6ca0b1300d3c96ce4b62e24724b`
- Ranxi `ranxi/production`: `a53a7ff163d9337a094e7537df3aac2b31f308b7`

## Directly absorbed

- BPS protocol compatibility in `backend/internal/service/basispoints`: response/input normalization, attachment protocol handling, tool catalog and repair, stream failure normalization, typed upstream failures, and usage merging.
- BPS image attachment diagnostics and safe error classification. Private upstream response content is excluded from client errors and persisted diagnostics.
- BPS in-band `401/403/429` handling. In-band `429` cools only the BPS route and never replays a generation after output was accepted; real HTTP `5xx` retains the existing account-health path.
- Existing local rolling image digest, native attachment capacity limits, random/fixed proxy selection, TLS profile propagation, usage accounting, and same-egress attachment/generation pinning remain enabled.

## Adapted

- Typed local egress failures survive the Responses and native-attachment `Forward` error wrappers as allowlisted sentinels, without exposing raw transport errors; the scheduler excludes them from account-health penalties and success recovery. BPS semantic failures are classified by `basispoints.UpstreamFailure` rather than matching arbitrary error text.
- Initial HTTP admission keeps the local bounded retry behavior and excludes continuation requests, committed responses, cancellation, exhausted budget, and the previous account.
- WebSocket/SSE acceleration is opt-in and limited to ordinary OAuth requests without continuation, passthrough, or plugin paths; typed handshake failures are preserved.
- The official Claude Code fallback fix is adapted for both OpenAI-compatible endpoints: a configured fallback group is eligible for selection, and channel mapping follows that group while billing remains with the API key group.
- Shared-pool JSON import now applies explicit exported BPS values ahead of form and global defaults; invalid values and privileged 403 group actions are rejected. Missing fields retain their previous default behavior.
- Mixed BPS/native scheduling tries eligible BPS capacity first, falls back to native capacity when BPS is full, and preserves explicit native ingress priority.

## Kept local

- Local random proxy runtime selection, TLS fingerprint context, rolling image digest, native image attachment chain, and existing usage/billing behavior take precedence over the upstream image-policy and Mihomo-specific paths.
- Existing Claude/panel RPM logic remains; the new upstream OpenAI RPM admission/scheduler chain is not used.

## Deferred

- Mihomo warm/recovery/acquisition and the associated image-policy, image-capacity, compaction, and generated-image API chain are deferred pending a complete dependency and runtime review.
- Deferred files include `openai_excel_bps_image_policy.go`, `openai_excel_bps_images.go`, `openai_excel_bps_recovery.go`, `openai_excel_bps_warm.go`, and their tests.
- Ranxi's later BPS/native capacity rebalancing depends on the deferred priority scheduling configuration, sticky selection, and shared scheduler. It is deferred together with that chain; ordinary requests keep the local BPS-first behavior.

## Verification evidence

- Passed: `go test -count=1 ./internal/service/basispoints`.
- Passed: no Go conflict markers under `backend` (`rg -n '^<<<<<<<|^=======|^>>>>>>>$' backend --glob '*.go'`).
- Passed: `git diff --check` for the BPS files touched in this pass.
- Final-SHA BPS service, mixed-pool scheduler, gateway fallback, shared import, routes, and API-contract targeted tests passed. Shared import creation assertions use an in-memory repository stub, not PostgreSQL; gateway fallback tests stop at an upstream 400 before usage persistence.
- The host Go default is `GOARCH=386`, which overflows constants in `internal/pkg/openaiauth`; final Go checks explicitly set `GOARCH=amd64` and limit parallel compilation.
