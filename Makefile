.PHONY: setup export

VENV := tools/venv
PYTHON := $(VENV)/bin/python

setup:
	@if [ ! -d tools/vendor/chromium ]; then \
		echo "tools/vendor/ is missing (Chromium binary, shared libs, poppler, fonts)."; \
		echo "This can't be rebuilt automatically — see tools/TOOLING.md for what's vendored and why."; \
		exit 1; \
	fi
	python3 -m venv $(VENV)
	$(VENV)/bin/pip install --quiet --upgrade pip
	$(VENV)/bin/pip install --quiet -r tools/requirements.txt
	@echo "Setup complete: $(PYTHON)"

export:
	@if [ -z "$(FILE)" ]; then \
		echo "Usage: make export FILE=path/to/chapter.html [OUT=path/to/chapter.pdf]"; \
		exit 1; \
	fi
	$(PYTHON) tools/export_pdf.py "$(FILE)" $(OUT)
