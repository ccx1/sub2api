# Non-BPS upstream decisions (2026-09-30)

## Sources and baseline

- Local pre-merge HEAD: `dcda8558019ce7e8a855d61f6251e357472c5059`.
- Official `old-origin/main`: `a0f41f95a07ee6ca0b1300d3c96ce4b62e24724b`.
- Ranxi `ranxi/production`: `a53a7ff163d9337a094e7537df3aac2b31f308b7`.
- This record covers the independent quality, operations, monitor, and account-administration modules handled in this pass. The BPS protocol decisions are in `bps-decisions.md`; shared routes, wiring, settings, account edit/import/bulk, and gateway paths are owned by the integration agents.

## Directly absorbed

- Account quality bulk deletion: retained the upstream interaction in `AccountQualityView.vue` and the six required `qualityOps` locale keys in both languages. The local quality-rule API, store, patch utility, and existing quality-rule behavior remain the baseline. This is UI-only until the integration owner verifies the existing delete API and focused test.

## Adapted

- The final-answer parser for the built-in Candy question is retained with focused tests. The pre-merge Pelican scheduler already recognized this exact legacy prompt and checked for `21`; the parser accepts equivalent final-answer formatting while rejecting contradictions and unrelated numbers. This adapts an existing Pelican behavior, independently of the deferred Candy monitor and group-test chain. Shared BPS and gateway adaptations are recorded separately.

## Kept local

- Quality operations: retained the local per-account rule contract and existing rule API/store; the proposed two-rule BPS coexistence would require a schema and behavior change, so the new coexistence migration and UI were removed.
- Guard v1, Ops error views, and Channel Monitor V2: retained the current local API, state, views, and tests. The proposed Guard v2 recovery model and Candy/status-card monitor paths were removed as incomplete separate feature chains.
- Settings: retained the local Excel BPS image-relay capacity controls. The upstream image policy, compact reserve, and warning limits were removed with their deferred runtime dependencies.

## Deferred

- OAuth reauth runtime/Mihomo worker, credential encryption, account auto-config/history, priority scheduling, Pelican group test/cost, Guard v2, quality templates/probes/BPS coexistence, model billing, account cost multiplier, and Candy monitor: each introduced new persistence, configuration, or cross-layer behavior that was not established against the local proxy, account protection, billing, and quality contracts. Their independent handlers, repositories, services, API wrappers, pages, locale modules, migrations, tools, and screenshots were removed from the merge tree. Re-evaluate only with a complete call-chain and migration review plus success/error/compatibility tests.
- The later OAuth `model_mapping_mode=aliases` repair is tied to auto-config writing alias mappings. That producer is absent from the integrated tree; current non-empty manual mappings retain their allowlist semantics. Revisit together with account create/edit, import defaults, shared-pool imports, bulk edit, cache and runtime admission tests.
- Ranxi's subsequent selectable re-login engines and parallel workers depend on the deferred reauth runtime, repository, migration and operations UI. This entire addition remains deferred.
- Channel Monitor V2 `HasSamples` and the new monitor presentation were not kept independently; the existing monitor implementation remains. A separate focused change can assess that fix without the Candy dependencies.
- The new auto-config BPS defaults UI and related account edit/import/bulk code require a decision from the BPS owner because they span shared account entry points. They are not included in this independent cleanup.
- The release workflow's reauth-runtime artifact job was removed because its `tools/reauth-runtime/build.sh` dependency was deferred; the local four-part release workflow remains separate. The new Kubernetes request-replica manifests and deployment settings were removed with their runtime-role contract. These can be reconsidered only after the full runtime, lifecycle, and deployment contract is integrated and verified.
- The Makefile keeps the existing bulk-edit test entry but drops four new entries whose test files are absent from this merge tree. The priority-scheduling and account-cost-multiplier documentation allowlist entries were removed with those deferred features. The BPS recovery screenshot allowlist remains for the BPS owner to decide with that feature.

## Integration and verification

- Removed stale locale imports and sidebar entries for deferred auto-config and priority scheduling; restored the quality operations document to the local one-rule schema after removing `254_quality_bps_coexist.sql`.
- `git diff --check` passed for the quality and settings files checked in this pass. An anchored conflict-marker scan found no conflict markers in tracked source; one existing literal separator line occurs inside an Antigravity test string.
- Final integration targeted checks passed: frontend account entry tests (328 cases), typecheck, i18n, production build, browser smoke for public pages, and backend BPS, fallback handler, API contract, and shared import tests. Authenticated admin shell and isolated PostgreSQL transaction paths were not exercised.
