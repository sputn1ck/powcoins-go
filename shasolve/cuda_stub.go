//go:build !cuda || !linux || !cgo

package shasolve

import (
	"context"
	"fmt"
)

type CUDASolver struct {
	BatchSize uint32
}

func (CUDASolver) Name() string { return "cuda" }

func (CUDASolver) Solve(context.Context, Job) (Result, error) {
	return Result{}, fmt.Errorf("cuda solver requires a linux cgo build with -tags cuda")
}
