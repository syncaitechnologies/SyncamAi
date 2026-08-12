# SyncCam AI Project Instructions

## Project identity

- Product name: SyncCam AI.
- Repository/workspace name: SyncamAi.
- This repository is the shared source of truth for product requirements, architecture, implementation, tests, operations, security, and decisions.
- The source documents use a precedence model. Decision logs and the contradiction analysis resolve conflicts; operational architecture documents take precedence over planning prose; when the documents are silent, record the decision in an ADR before implementation.

## Documentation source of truth

Read the relevant documents before making product or architecture changes:

- `PRD-SyncCam-AI.md` — product scope, personas, requirements, metrics, and release scope.
- `ARCHITECTURE.md` — system, service, event, cloud, data, security, and deployment architecture.
- `AI-ARCHITECTURE.md` — AI module registry, model strategy, inference, training, and evaluation.
- `BACKEND-ARCHITECTURE-SyncCam-AI.md` — schemas, APIs, events, storage, and backend contracts.
- `UX-DESIGN-SyncCam-AI.md` — UX principles, screens, flows, accessibility, and acceptance criteria.
- `SECURITY-PRIVACY-COMPLIANCE-AI-GOVERNANCE.md` — security, privacy, compliance, AI governance, and release gates.
- `DEVOPS-MLOPS-SyncCam-AI.md` — CI/CD, infrastructure, observability, MLOps, and cost controls.
- `BUSINESS-MODEL-STRATEGY-SyncCam-AI.md` — market, pricing, GTM, economics, and commercial assumptions.
- `ENGINEERING-EXECUTION-ROADMAP-SyncCam-AI.md` — execution plan, task IDs, sequencing, and monorepo layout.
- `01-contradiction-analysis.md` — resolved conflicts and canonical values.
- `02-deduplication-map.md` — canonical home for duplicated facts.

Do not silently invent a resolution when these documents disagree. State the conflict, apply the precedence rules, and record a decision or update the appropriate canonical document.

## Canonical decisions already recorded

- `edge-agent` is a Go single binary.
- AI-plane services use Python 3.12.
- The MVP engineering scope is 12 AI engines, including event-only vehicle classes and the camera-health capability.
- Event streams use Kinesis/MSK; task queues use RabbitMQ; alert fast-path and fan-out use SQS/SNS.
- Postgres is the relational source of truth; other stores are projections or derived indexes.
- Do not rename API domains, webhook routes, or wire-level headers as part of a branding change without an explicit API migration decision.

## Working rules

- Keep changes small, reviewable, and linked to a requirement or roadmap task.
- Update documentation and tests when behavior or contracts change.
- Never commit secrets, credentials, customer footage, biometric data, model weights, or generated evidence.
- Use feature branches and pull requests; keep `main` stable.
- For security, privacy, compliance, licensing, and AI-safety changes, consult the security/governance document and flag items requiring legal or human approval.
- Treat assumptions as assumptions and do not present them as validated facts.
