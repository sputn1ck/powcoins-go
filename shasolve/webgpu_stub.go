//go:build js

package shasolve

import (
	"context"
	"fmt"
)

type WebGPUSolver struct {
	BatchSize uint32
}

func (*WebGPUSolver) Name() string { return "webgpu" }

func (*WebGPUSolver) Solve(context.Context, Job) (Result, error) {
	return Result{}, fmt.Errorf("native WebGPU solver is unavailable for js builds")
}
