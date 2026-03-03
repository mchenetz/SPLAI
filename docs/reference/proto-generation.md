# Proto Generation

## Source of truth

- `proto/splai/v1/*.proto`

## Generation path

- `buf.yaml`
- `buf.gen.yaml`
- `scripts/gen-proto.sh`
- Output directory: `gen/proto/`

## Generated artifacts

Code generation writes protobuf and gRPC Go files under:

- `gen/proto/splai/v1/*.pb.go`
- `gen/proto/splai/v1/*_grpc.pb.go`

## Generate when tools are available

```bash
make proto
```
