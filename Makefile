PYTHON ?= python3
.PHONY: check test
check:
	$(PYTHON) scripts/check_repo.py
	$(PYTHON) -m unittest discover -s tests -v
	@for script in .specify/scripts/bash/*.sh scripts/*.sh; do bash -n "$$script" || exit; done
	@SPECIFY_FEATURE_DIRECTORY=specs/001-workspace-lifecycle SPECIFY_FEATURE_NO_PERSIST=1 .specify/scripts/bash/check-prerequisites.sh --json --require-tasks --include-tasks

test:
	$(PYTHON) -m unittest discover -s tests -v
