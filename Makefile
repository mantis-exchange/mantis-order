PROTO_DIR := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))/../mantis-common/proto
OUT_DIR := pkg/proto
GO_PKG := github.com/mantis-exchange/mantis-order/pkg/proto/mantis

.PHONY: proto tools

tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.4.0

proto: tools
	@mkdir -p $(OUT_DIR)/mantis
	protoc \
		--proto_path=$(PROTO_DIR) \
		--go_out=$(OUT_DIR) --go_opt=paths=source_relative \
		--go_opt=Mmantis/types.proto=$(GO_PKG) \
		--go_opt=Mmantis/matching.proto=$(GO_PKG) \
		--go_opt=Mmantis/order.proto=$(GO_PKG) \
		--go-grpc_out=$(OUT_DIR) --go-grpc_opt=paths=source_relative \
		--go-grpc_opt=Mmantis/types.proto=$(GO_PKG) \
		--go-grpc_opt=Mmantis/matching.proto=$(GO_PKG) \
		--go-grpc_opt=Mmantis/order.proto=$(GO_PKG) \
		$(PROTO_DIR)/mantis/types.proto \
		$(PROTO_DIR)/mantis/matching.proto \
		$(PROTO_DIR)/mantis/order.proto
