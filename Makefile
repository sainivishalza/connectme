.PHONY: hooks check
## hooks: use the repository's git hooks (secret scan, gofmt, go vet before every commit)
hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
	@echo "git hooks installed from .githooks"

## check: the checks every change must pass before it is finished
check:
	cd backend && go build ./... && go vet ./... && go test -race -p 1 ./...
	cd mobile && flutter analyze && flutter test
	cd ai-service && pytest -q
	cd realtime && npm test
	cd backend && govulncheck ./...
