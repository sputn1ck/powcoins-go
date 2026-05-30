package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sputn1ck/webgpu-sha/shasolve"
)

func main() {
	var (
		difficulty = flag.Uint("difficulty", 28, "powcoin trailing-zero difficulty")
		backends   = flag.String("backends", "metal,webgpu", "comma-separated backends: metal,webgpu,cpu")
		batchSize  = flag.Uint("batch-size", 0, "nonces per GPU batch; 0 uses the backend default")
		startNonce = flag.Uint("start-nonce", 0, "starting nonce")
		limit      = flag.Uint64("limit", 0, "maximum hashes per backend; 0 searches the uint32 nonce space")
		timeout    = flag.Duration("timeout", 10*time.Minute, "timeout per backend")
	)
	flag.Parse()

	job := shasolve.DefaultBenchmarkJob(uint32(*difficulty))
	if *difficulty == 28 && *startNonce == 0 && *limit == 0 {
		job = shasolve.Difficulty28BenchmarkJob()
	}
	if *startNonce != 0 {
		job.StartNonce = uint32(*startNonce)
	}
	if *batchSize != 0 {
		job.BatchSize = uint32(*batchSize)
	}
	if *limit != 0 {
		job.Limit = *limit
	}

	for _, name := range strings.Split(*backends, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		solver, err := solverByName(name, uint32(*batchSize))
		if err != nil {
			log.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		fmt.Printf("running backend=%s difficulty=%d start=%d limit=%d batch=%d\n",
			solver.Name(), job.Difficulty, job.StartNonce, job.Limit, job.BatchSize)
		result, err := solver.Solve(ctx, job)
		cancel()
		if err != nil {
			log.Fatalf("%s: %v", solver.Name(), err)
		}
		if zeros, ok := shasolve.Verify(job, result.Nonce); !ok {
			log.Fatalf("%s: invalid nonce %d trailing_zero_bits=%d", solver.Name(), result.Nonce, zeros)
		}
		device := result.Device
		if device == "" {
			device = solver.Name()
		}
		fmt.Printf("solved backend=%s device=%q nonce=%d tries=%d elapsed=%.3fs rate=%.2f MH/s trailing_zero_bits=%d\n",
			result.Backend, device, result.Nonce, result.Tries, result.Elapsed.Seconds(),
			result.HashesPerSecond/1_000_000, result.ZeroBits)
	}
}

func solverByName(name string, batchSize uint32) (shasolve.Solver, error) {
	switch strings.ToLower(name) {
	case "metal":
		return shasolve.MetalSolver{BatchSize: batchSize}, nil
	case "webgpu", "wgpu":
		return &shasolve.WebGPUSolver{BatchSize: batchSize}, nil
	case "cpu":
		return shasolve.CPUSolver{}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q", name)
	}
}
