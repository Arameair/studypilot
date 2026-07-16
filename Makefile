.PHONY: build test race vet python-test shell-check verify

build:
	go build -o bin/studypilot ./cmd/studypilot

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...
	go list ./...

python-test:
	PYTHONPYCACHEPREFIX=/tmp/studypilot-python-cache python3 -m unittest discover -s tools/transcription-worker/tests -p 'test_*.py'
	PYTHONPYCACHEPREFIX=/tmp/studypilot-python-cache python3 -m py_compile tools/transcription-worker/worker.py tools/transcription-worker/tests/test_worker.py

shell-check:
	bash -n scripts/setup-transcription-worker.sh
	bash -n scripts/validate-transcription-worker.sh
	bash -n scripts/validate-transcription-workflow.sh
	bash -n scripts/validate-gui-workflow.sh
	bash -n scripts/validate-local-capture.sh

verify: test race vet build python-test shell-check
	git diff --check
