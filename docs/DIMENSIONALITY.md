# Dimensionality study

What do 10,000 bits buy over smaller hypervectors? Every number below is measured by
`TestDimensionPoint` compiled at each dimension (`scripts/dimscan.sh`; build tags
`hd_d1024`/`hd_d2048`/`hd_d4096`), same machine, same seeds, same code. The default
build stays 10,000 — study builds are measurement instruments, not supported
configurations (goldens skip; file formats embed the dimension and refuse mismatches).

## Cost per operation

| D | bytes/vec | bind | Hamming | permute | Bundle8 |
| --: | --: | --: | --: | --: | --: |
| 1024 | 128 | 4 ns | 4 ns | 24 ns | 212 ns |
| 2048 | 256 | 9 ns | 8 ns | 49 ns | 467 ns |
| 4096 | 512 | 20 ns | 15 ns | 103 ns | 960 ns |
| 10000 | 1256 | 73 ns | 41 ns | 279 ns | 2667 ns |

## Routing accuracy vs dimension

| D | curated canonical | curated paraphrase | banking77 | clinc150 |
| --: | --: | --: | --: | --: |
| 1024 | 95.6% | 24.4% | 60.7% | 65.3% |
| 2048 | 95.6% | 35.6% | 64.7% | 68.9% |
| 4096 | 97.8% | 40.0% | 67.2% | 70.4% |
| 10000 | 100.0% | 37.8% | 69.1% | 71.8% |

## Capacity (G0 random-context regime: K pairs, 4-action cleanup)

| D | K=16 | K=32 | K=64 | K=128 | K=256 | K=512 |
| --: | --: | --: | --: | --: | --: | --: |
| 1024 | 100.0% (m≥55) | 100.0% (m≥22) | 93.8% (m≥4) | 89.8% (m≥0) | 74.6% (m≥0) | 60.5% (m≥0) |
| 2048 | 100.0% (m≥154) | 100.0% (m≥75) | 100.0% (m≥7) | 97.7% (m≥0) | 87.5% (m≥0) | 76.0% (m≥0) |
| 4096 | 100.0% (m≥323) | 100.0% (m≥161) | 100.0% (m≥87) | 100.0% (m≥31) | 98.0% (m≥0) | 87.5% (m≥0) |
| 10000 | 100.0% (m≥841) | 100.0% (m≥475) | 100.0% (m≥320) | 100.0% (m≥151) | 100.0% (m≥61) | 98.4% (m≥2) |

Read it straight: accuracy and capacity columns state exactly what was measured — where a
smaller dimension holds recall, the extra bits were headroom for THAT load, not free
accuracy; where it collapses, that is the superposition floor arriving early. Per-op cost
scales with words (D/64), so the speed/size win of a smaller D is mechanical.
