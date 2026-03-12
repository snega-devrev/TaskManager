# Generate Go and gRPC code from api/proto/taskmanager.proto.
# Requires: protoc, protoc-gen-go, protoc-gen-go-grpc (go install ...)
.PHONY: generate
generate:
	export PATH="$$(go env GOPATH)/bin:$$PATH" && \
	protoc -I . \
		--go_out=. --go_opt=module=taskmanager \
		--go-grpc_out=. --go-grpc_opt=module=taskmanager \
		api/proto/taskmanager.proto
