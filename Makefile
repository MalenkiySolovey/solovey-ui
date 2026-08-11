PS ?= powershell -NoProfile -ExecutionPolicy Bypass
RUN = $(PS) -File tests/baseline/run-command.ps1 -ContinueOnError

.PHONY: audit audit\:lint-go audit\:vet audit\:build audit\:test-go audit\:test-go-race audit\:cover audit\:gosec audit\:vuln audit\:fe-typecheck audit\:fe-lint audit\:fe-build audit\:test-fe audit\:e2e audit\:fe-install

audit: audit\:build audit\:vet audit\:test-go audit\:test-go-race audit\:cover audit\:gosec audit\:vuln audit\:lint-go audit\:fe-typecheck audit\:fe-lint audit\:fe-build audit\:test-fe audit\:e2e

audit\:lint-go:
	$(RUN) -Group analysis -Name staticcheck -CommandLine "staticcheck ./..."
	$(RUN) -Group analysis -Name golangci-lint -CommandLine "golangci-lint run"

audit\:vet:
	$(RUN) -Group core -Name go-vet -CommandLine "go vet ./..."

audit\:build:
	$(RUN) -Group core -Name go-build -CommandLine "go build ./..."

audit\:test-go:
	$(RUN) -Group core -Name go-test -CommandLine "go test ./..."

audit\:test-go-race:
	$(RUN) -Group core -Name go-test-race -CommandLine "go test ./... -race -count=1"

audit\:cover:
	$(RUN) -Group core -Name go-cover -CommandLine "go test ./... -coverprofile tests/baseline/core/coverage.out"

audit\:gosec:
	$(RUN) -Group analysis -Name gosec -CommandLine "gosec -exclude-dir .gotmp -exclude-dir .gocache -exclude-dir frontend/node_modules ./..."

audit\:vuln:
	$(RUN) -Group analysis -Name govulncheck -CommandLine "govulncheck ./..."

audit\:fe-install:
	$(RUN) -Group core -Name npm-ci -WorkingDirectory frontend -CommandLine "npm ci"

audit\:fe-typecheck:
	$(RUN) -Group core -Name npm-run-typecheck -WorkingDirectory frontend -SkipReason "frontend/package.json does not define a typecheck script; build runs vue-tsc --noEmit."

audit\:fe-lint:
	$(RUN) -Group core -Name npm-run-lint -WorkingDirectory frontend -CommandLine "npm run lint"

audit\:fe-build:
	$(RUN) -Group core -Name npm-run-build -WorkingDirectory frontend -CommandLine "npm run build"

audit\:test-fe:
	$(RUN) -Group core -Name npm-run-test -WorkingDirectory frontend -CommandLine "npm run test"

audit\:e2e:
	$(RUN) -Group core -Name e2e -SkipReason "The end-to-end suite runs in its dedicated workflow."
