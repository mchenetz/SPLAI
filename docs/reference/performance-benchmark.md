# Scheduler Performance Benchmark

This project includes a baseline benchmark harness for scheduler assignment path cost.

## Run

```bash
./scripts/benchmark-scheduler.sh
```

Scale-oriented run (configurable workers):

```bash
WORKERS=1000 ./scripts/scale-proof.sh
```

NFR utilization proof (default 10k workers / 20k jobs):

```bash
WORKERS=10000 JOBS=20000 TARGET_UTIL=0.70 ./scripts/nfr-proof.sh
```

## Output

Results are written to:

- `docs/release/benchmarks/scheduler-<timestamp>.txt`
- `docs/release/benchmarks/scale-proof-<timestamp>.txt`
- `docs/release/benchmarks/nfr-proof-<timestamp>.txt`

Use this artifact in release validation to track assignment-path regressions.
