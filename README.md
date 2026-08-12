# SyncCam AI

SyncCam AI is an edge-first AI video intelligence platform for security, safety, attendance, compliance, and analytics using existing CCTV infrastructure.

This repository is the shared source of truth for the team. It contains the product specifications and will become the implementation monorepo described in the engineering roadmap.

The Phase 0 foundation lives beside the root specifications:

- [`docs/adr/`](docs/adr/) — accepted and proposed architecture decisions.
- [`traceability/`](traceability/) — executable MVP requirement and active-task mappings.
- [`shared/contracts/`](shared/contracts/) — OpenAPI, Avro, and Protobuf compatibility boundaries.
- `backend/`, `edge/`, `ai-services/`, and `frontend/` — compile-only language scaffolds.
- `infrastructure/terraform/sandbox/` and `compose.yaml` — a non-deploying cloud scaffold and local dependencies.

Run the complete verification gate with `make verify` on macOS/Linux or `pwsh scripts/verify.ps1` on Windows. Required runtimes are Go 1.22+, Python 3.12, Node.js 22, and pnpm 11.16.0.

The source-publication license and production model licenses remain human/legal gates; see [`ADR-001`](docs/adr/ADR-001-model-license.md) and the [`license policy`](licenses/README.md).

## Project instructions

- [`AGENTS.md`](AGENTS.md) contains the project-wide instructions for Codex and other coding agents.
- [`CLAUDE.md`](CLAUDE.md) points Claude to the same canonical instructions.

Read the relevant specification before changing requirements, architecture, APIs, UX, security, operations, business assumptions, or the engineering roadmap.

## Documentation map

| Document | Purpose |
|---|---|
| [`PRD-SyncCam-AI.md`](PRD-SyncCam-AI.md) | Product requirements, personas, scope, metrics, and release plan |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | System, services, events, cloud, data, security, and deployment architecture |
| [`AI-ARCHITECTURE.md`](AI-ARCHITECTURE.md) | AI module registry, models, inference, training, and evaluation |
| [`BACKEND-ARCHITECTURE-SyncCam-AI.md`](BACKEND-ARCHITECTURE-SyncCam-AI.md) | Database schemas, APIs, events, storage, and backend contracts |
| [`UX-DESIGN-SyncCam-AI.md`](UX-DESIGN-SyncCam-AI.md) | UX principles, screens, flows, accessibility, and acceptance criteria |
| [`SECURITY-PRIVACY-COMPLIANCE-AI-GOVERNANCE.md`](SECURITY-PRIVACY-COMPLIANCE-AI-GOVERNANCE.md) | Security, privacy, compliance, AI governance, and release gates |
| [`DEVOPS-MLOPS-SyncCam-AI.md`](DEVOPS-MLOPS-SyncCam-AI.md) | CI/CD, infrastructure, observability, MLOps, and cost controls |
| [`BUSINESS-MODEL-STRATEGY-SyncCam-AI.md`](BUSINESS-MODEL-STRATEGY-SyncCam-AI.md) | Market, pricing, go-to-market, economics, and commercial assumptions |
| [`ENGINEERING-EXECUTION-ROADMAP-SyncCam-AI.md`](ENGINEERING-EXECUTION-ROADMAP-SyncCam-AI.md) | Execution plan, task IDs, sequencing, and monorepo layout |
| [`01-contradiction-analysis.md`](01-contradiction-analysis.md) | Resolved conflicts and canonical values |
| [`02-deduplication-map.md`](02-deduplication-map.md) | Canonical home for duplicated facts |

## Team workflow

1. Clone the repository or open it in GitHub Codespaces.
2. Create a feature branch for each change.
3. Read `AGENTS.md` and the relevant specifications.
4. Implement the change, update documentation/tests, and open a pull request.
5. Keep `main` stable and use pull-request review for shared decisions.

Technical identifiers such as existing API domains, webhook routes, and wire-level headers are intentionally preserved during the product-brand rename. Change them only through an explicit API migration decision.
