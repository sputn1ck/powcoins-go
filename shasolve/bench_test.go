package shasolve

import (
	"context"
	"testing"
	"time"
)

func BenchmarkDifficulty28Metal(b *testing.B) {
	benchmarkDifficulty28(b, MetalSolver{BatchSize: DefaultBatchSize})
}

func BenchmarkDifficulty28WebGPU(b *testing.B) {
	benchmarkDifficulty28(b, &WebGPUSolver{BatchSize: DefaultBatchSize})
}

func benchmarkDifficulty28(b *testing.B, solver Solver) {
	job := Difficulty28BenchmarkJob()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for i := 0; i < b.N; i++ {
		start := time.Now()
		result, err := solver.Solve(ctx, job)
		if err != nil {
			b.Fatal(err)
		}
		if _, ok := Verify(job, result.Nonce); !ok {
			b.Fatalf("%s returned invalid nonce %d", solver.Name(), result.Nonce)
		}
		b.ReportMetric(result.HashesPerSecond/1_000_000, "MH/s")
		b.ReportMetric(float64(result.Tries), "hashes")
		b.ReportMetric(float64(time.Since(start).Milliseconds()), "ms/solve")
	}
}
