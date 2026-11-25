package arena

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestArenaRoutingRefresh is the opt-in Go worker for routing_refresh.py.
// The driver supplies the identical training data, warmup and query order to
// both runtimes and measures each router in isolation.
func TestArenaRoutingRefresh(t *testing.T) {
	input := os.Getenv("ARENA_REFRESH_INPUT")
	if input == "" {
		t.Skip("run routing_refresh.py for the paired benchmark")
	}
	output := os.Getenv("ARENA_REFRESH_OUTPUT")
	if output == "" {
		t.Fatal("ARENA_REFRESH_OUTPUT is required")
	}
	var work struct {
		Classes []Intent `json:"classes"`
		Warmup  []string `json:"warmup"`
		Test    []string `json:"test"`
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &work); err != nil {
		t.Fatal(err)
	}
	if len(work.Classes) == 0 || len(work.Warmup) == 0 || len(work.Test) == 0 {
		t.Fatal("benchmark worker requires classes, warmup and test rows")
	}
	c := NewClassifier()
	c.Train(work.Classes)
	for _, text := range work.Warmup {
		c.Route(text)
	}
	type sample struct {
		Pred string `json:"prediction"`
		NS   int64  `json:"ns"`
	}
	samples := make([]sample, len(work.Test))
	for i, text := range work.Test {
		start := time.Now()
		prediction, _ := c.Route(text)
		samples[i] = sample{prediction, time.Since(start).Nanoseconds()}
	}
	data, err = json.Marshal(samples)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
}
