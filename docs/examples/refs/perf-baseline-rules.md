<perf-baseline-rules>
Performance work without a baseline is theatre. Measure first.

**The cycle:**
1. **Baseline.** Run the current code under a representative workload. Record: p50/p95/p99 latency, allocations/op, CPU time, peak RSS. Without numbers, "it feels slow" is not actionable.
2. **Identify the hot path.** Profile (pprof / perf / flame graph). The bottleneck is rarely where you expect — code intuition is unreliable.
3. **Hypothesise one change.** State expected gain ("inlining will shave ~15% off allocs"). If you can't predict the magnitude, you don't understand the bottleneck yet.
4. **Implement minimally.** No drive-by refactors.
5. **Re-measure.** Compare new numbers to baseline. If gain < 5% on the metric you targeted, revert — complexity isn't worth it. If gain came with regression elsewhere (e.g. memory up, latency down), surface the trade-off explicitly.

**Anti-patterns:**
- Optimising without a profile. "Foo is slow because it does X" without measurement = guess.
- Micro-optimising cold paths. If a function runs 0.1% of CPU time, halving it saves 0.05% — not worth.
- Adding caches without measuring cache hit rate. Bad caches are slower than no cache (extra lookup + invalidation cost).
- Optimising in test environments unlike prod (small datasets, single-thread, no concurrency).
- Reporting "X is faster" without comparison numbers and methodology.

**Acceptable wins (worth the complexity):**
- ≥ 20% latency reduction on a hot path.
- ≥ 50% allocation reduction on a hot path.
- Eliminating an O(n²) or worse.
- Removing a CPU pin that prevents horizontal scaling.

**Unacceptable wins (revert):**
- < 5% gain at the cost of readability.
- "Theoretically faster" without empirical confirmation.
- Gains that depend on workload characteristics you can't guarantee in prod.
</perf-baseline-rules>
