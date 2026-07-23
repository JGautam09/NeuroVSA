## What & why

Briefly describe the change and the motivation.

## Checklist

- [ ] This PR targets `main` from a fork or a non-`main` branch.
- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` passes
- [ ] `go test -race ./...` passes
- [ ] New core primitives include an equivalence test vs. a naive reference
- [ ] Core packages remain dependency-free (only `gorilla/websocket` allowed, in `api/`)
- [ ] Docs/benchmarks updated if behavior or numbers changed
- [ ] I agree that this contribution may be distributed under the repository's MIT License.

## Notes

Anything reviewers should know (benchmark deltas, follow-ups, trade-offs, or API/security
impact).
