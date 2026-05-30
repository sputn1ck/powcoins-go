package shasolve

import (
	"context"
	"testing"
	"time"
)

func TestCPUSolverLowDifficulty(t *testing.T) {
	job := DefaultBenchmarkJob(12)
	job.Limit = 1 << 20
	result, err := CPUSolver{}.Solve(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Verify(job, result.Nonce); !ok {
		t.Fatalf("invalid nonce %d", result.Nonce)
	}
}

func TestMetalSolverLowDifficulty(t *testing.T) {
	job := DefaultBenchmarkJob(12)
	job.Limit = 1 << 20
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	result, err := (MetalSolver{BatchSize: 1 << 16}).Solve(ctx, job)
	if err != nil {
		t.Skipf("metal solver unavailable: %v", err)
	}
	if _, ok := Verify(job, result.Nonce); !ok {
		t.Fatalf("invalid nonce %d", result.Nonce)
	}
}

func TestWebGPUSolverLowDifficulty(t *testing.T) {
	job := DefaultBenchmarkJob(12)
	job.Limit = 1 << 20
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	result, err := (&WebGPUSolver{BatchSize: 1 << 16}).Solve(ctx, job)
	if err != nil {
		t.Skipf("webgpu solver unavailable: %v", err)
	}
	if _, ok := Verify(job, result.Nonce); !ok {
		t.Fatalf("invalid nonce %d", result.Nonce)
	}
}
