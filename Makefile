format: .bin/ory node_modules   # formats the source code
	.bin/ory dev headers copyright --type=open-source
	gofmt -l -s -w .
	npm exec -- prettier --write .

lint:  # runs golangci-lint
	@echo "Linting..."
	golangci-lint run --config=.golangci.yml ./...

licenses: .bin/licenses node_modules  # checks open-source licenses
	.bin/licenses

.bin/licenses: Makefile
	curl https://raw.githubusercontent.com/ory/ci/master/licenses/install | sh

.bin/ory: Makefile
	curl https://raw.githubusercontent.com/ory/meta/master/install.sh | bash -s -- -b .bin ory v0.3.2
	touch .bin/ory

node_modules: package-lock.json
	npm install
	touch node_modules

test:
	go vet ./...
	go test -covermode=atomic -coverprofile="coverage.out" .
