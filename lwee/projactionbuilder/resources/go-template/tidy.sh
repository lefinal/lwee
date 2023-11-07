#!/bin/bash

export GOCACHE=/tmp/go-build-cache
export GOPATH=/tmp/go

go mod tidy
go fmt
go get -u github.com/lefinal/lwee/go-sdk
go mod tidy
