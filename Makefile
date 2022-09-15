format: node_modules   # formats the source code
	npm exec -- prettier --write .
	gofmt -l -s -w .

help:
	cat Makefile | grep '^[^ ]*:' | grep -v '^\.bin/' | grep -v '.SILENT:' | grep -v '^node_modules:' | grep -v help | sed 's/:.*#/#/' | column -s "#" -t

node_modules: package-lock.json
	npm install
	touch node_modules


.SILENT:
.DEFAULT_GOAL := help
