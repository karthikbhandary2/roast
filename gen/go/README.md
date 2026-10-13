# Generated code - do not hand-edit

This directory is populated by running `make proto` (which shells out to
`buf generate`, per `buf.gen.yaml` at the repo root). It mirrors the package
layout of `proto/`:

```
gen/go/common/v1/common.pb.go
gen/go/user/v1/user.pb.go
gen/go/user/v1/user_grpc.pb.go
gen/go/product/v1/...
gen/go/search/v1/...
gen/go/order/v1/...
gen/go/payment/v1/...
gen/go/notification/v1/...
```

It's intentionally empty in this scaffold - run `make proto` locally (needs
network access to buf.build's remote plugins, or a local `protoc` +
`protoc-gen-go` + `protoc-gen-go-grpc` toolchain) to populate it before
building any service that imports these types.
