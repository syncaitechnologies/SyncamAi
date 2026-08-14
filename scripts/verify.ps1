$ErrorActionPreference = "Stop"

function Assert-NativeSuccess {
    if ($LASTEXITCODE -ne 0) {
        throw "Native verification command failed with exit code $LASTEXITCODE."
    }
}

python scripts/validate_markdown_links.py
Assert-NativeSuccess
python scripts/validate_task_references.py
Assert-NativeSuccess
python scripts/validate_licenses.py
Assert-NativeSuccess
python scripts/validate_secrets.py
Assert-NativeSuccess
python scripts/validate_contracts.py
Assert-NativeSuccess
python scripts/validate_contract_compatibility.py
Assert-NativeSuccess
python scripts/validate_traceability.py
Assert-NativeSuccess
python scripts/validate_coverage_policy.py
Assert-NativeSuccess
python -m compileall -q ai-services/src ai-services/tests
Assert-NativeSuccess
python -m unittest discover -s ai-services/tests -v
Assert-NativeSuccess
python -m unittest discover -s tests/validators -v
Assert-NativeSuccess

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go 1.25.13+ is required to run the complete verification suite."
}

New-Item -ItemType Directory -Path coverage -Force | Out-Null
go test "-coverprofile=coverage/backend.out" ./backend/internal/...
Assert-NativeSuccess
python scripts/validate_go_coverage.py coverage/backend.out 80
Assert-NativeSuccess
go test ./backend/cmd/... ./edge/...
Assert-NativeSuccess
pnpm --dir frontend/apps/web check
Assert-NativeSuccess
pnpm --dir frontend/apps/web build
Assert-NativeSuccess
