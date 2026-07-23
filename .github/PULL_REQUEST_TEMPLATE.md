## What & why

Briefly describe the change and the motivation.

## Checklist

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` passes
- [ ] `go test -race ./...` passes
- [ ] New core primitives include an equivalence test vs. a naive reference
- [ ] Core packages remain dependency-free (only `gorilla/websocket` allowed, in `api/`)
- [ ] Docs/benchmarks updated if behavior or numbers changed

## Notes

Anything reviewers should know (benchmark deltas, follow-ups, trade-offs).
