//go:build !darwin || !cgo

package shasolve

import (
	"context"
	"fmt"
)

type MetalSolver struct {
	BatchSize uint32
}

func (MetalSolver) Name() string { return "metal" }

func (MetalSolver) Solve(context.Context, Job) (Result, error) {
	return Result{}, fmt.Errorf("metal solver requires darwin with cgo")
}
