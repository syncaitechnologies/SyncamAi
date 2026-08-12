$ErrorActionPreference = "Stop"

python scripts/validate_markdown_links.py
python scripts/validate_task_references.py
python scripts/validate_licenses.py
python scripts/validate_secrets.py
python scripts/validate_contracts.py
python scripts/validate_contract_compatibility.py
python scripts/validate_traceability.py
python scripts/validate_coverage_policy.py
python -m compileall -q ai-services/src ai-services/tests
python -m unittest discover -s ai-services/tests -v
python -m unittest discover -s tests/validators -v

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go 1.22+ is required to run the complete verification suite."
}

go test ./backend/... ./edge/...
pnpm --dir frontend/apps/web check
pnpm --dir frontend/apps/web build
