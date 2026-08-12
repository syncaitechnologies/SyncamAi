PYTHON ?= python3
PNPM ?= pnpm

.PHONY: verify docs contracts traceability python go web

verify: docs contracts traceability python go web

docs:
	$(PYTHON) scripts/validate_markdown_links.py
	$(PYTHON) scripts/validate_task_references.py
	$(PYTHON) scripts/validate_licenses.py
	$(PYTHON) scripts/validate_secrets.py

contracts:
	$(PYTHON) scripts/validate_contracts.py
	$(PYTHON) scripts/validate_contract_compatibility.py

traceability:
	$(PYTHON) scripts/validate_traceability.py
	$(PYTHON) scripts/validate_coverage_policy.py

python:
	$(PYTHON) -m compileall -q ai-services/src ai-services/tests
	$(PYTHON) -m unittest discover -s ai-services/tests -v
	$(PYTHON) -m unittest discover -s tests/validators -v

go:
	go test ./backend/... ./edge/...

web:
	$(PNPM) --dir frontend/apps/web check
	$(PNPM) --dir frontend/apps/web build
