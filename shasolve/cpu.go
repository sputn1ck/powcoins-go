package shasolve

import (
	"context"
	"fmt"
	"time"
)

type CPUSolver struct{}

func (CPUSolver) Name() string { return "cpu" }

func (CPUSolver) Solve(ctx context.Context, job Job) (Result, error) {
	job = NormalizeJob(job)
	if err := checkDifficulty(job.Difficulty); err != nil {
		return Result{}, err
	}

	started := time.Now()
	limit := job.Limit
	if limit == 0 {
		limit = uint64(NotFound) - uint64(job.StartNonce) + 1
	}
	var tries uint64
	for nonce := job.StartNonce; tries < limit; nonce++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		tries++
		if zeros, ok := Verify(job, nonce); ok {
			res := result("cpu", "cpu", job, nonce, tries, started)
			res.ZeroBits = zeros
			return res, nil
		}
		if nonce == NotFound {
			break
		}
	}
	return Result{}, fmt.Errorf("no solution in %d hashes", tries)
}
