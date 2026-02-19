# Proto Generation

## Source of truth

- `proto/daef/v1/*.proto`

## Generation path

- `buf.yaml`
- `buf.gen.yaml`
- `scripts/gen-proto.sh`
- Output directory: `gen/proto/`

## Checked-in stubs

- `gen/proto/daef/v1/stubs.pb.go`

These stubs provide compile-time Go message/service types in environments where external generation tools are unavailable.

## Generate when tools are available

```bash
make proto
```
