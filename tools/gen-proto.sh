#!/bin/bash

set ee

echo "protoc version:"
protoc --version
(
cd proto
for directory in ./*/ ; do
  echo "generating for $directory..."
  protoc \
      --go_out="${PWD}/.." \
      --go_opt=module=github.com/lefinal/lwee \
      --go-grpc_out="${PWD}/.." \
      --go-grpc_opt=module=github.com/lefinal/lwee \
      --proto_path=. \
      "$directory"/*.proto
done
)
