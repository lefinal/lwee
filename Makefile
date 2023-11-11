.PHONY: all dep test coverage coverhtml lint

all: dep test race msan lint

lint: ## Lint the files
	(cd ./lwee && revive -config ../revive.toml ./...)
	(cd ./go-sdk && revive -config ../revive.toml ./...)

test: ## Run unittests
	(cd ./lwee && go test -tags=e2e -v ./...)

race: dep ## Run data race detector
	(cd ./lwee && go test -race -short ./...)

msan: dep ## Run memory sanitizer
	(cd ./lwee && CC=clang CXX=clang++ go test -msan -short ./...)

coverhtml: ## Generate global code coverage report in HTML
	(cd ./lwee && go test -race -coverprofile=coverage.txt -covermode=atomic ./...)

dep: ## Get the dependencies
	(cd ./lwee && go get -v -d ./...)
	(cd ./go-sdk && go get -v -d ./...)
	# Add repo for installing Podman 4.
	echo 'deb http://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/unstable/xUbuntu_22.04/ /' | sudo tee /etc/apt/sources.list.d/devel:kubic:libcontainers:unstable.list
	curl -fsSL https://download.opensuse.org/repositories/devel:kubic:libcontainers:unstable/xUbuntu_22.04/Release.key | gpg --dearmor | sudo tee /etc/apt/trusted.gpg.d/devel_kubic_libcontainers_unstable.gpg > /dev/null
	sudo apt update ; sudo apt install -y \
   	   podman \
   	   libbtrfs-dev \
   	   libgpgme-dev \
   	   libdevmapper-dev
