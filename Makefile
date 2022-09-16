format: node_modules   # formats the source code
	gofmt -l -s -w .
	npm exec -- prettier --write .

help:
	cat Makefile | grep '^[^ ]*:' | grep -v '^\.bin/' | grep -v '.SILENT:' | grep -v '^node_modules:' | grep -v help | sed 's/:.*#/#/' | column -s "#" -t

node_modules: package-lock.json
	npm install
	touch node_modules

test:
	go mod tidy
	go vet -x .
	go test -covermode=atomic -coverprofile="coverage.out" .


.DEFAULT_GOAL := help
