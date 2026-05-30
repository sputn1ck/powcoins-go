//go:build cuda && linux && cgo

package shasolve

/*
#cgo LDFLAGS: -ldl
#include "cuda_impl.h"
*/
import "C"

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

type CUDASolver struct {
	BatchSize uint32
}

func (CUDASolver) Name() string { return "cuda" }

func (s CUDASolver) Solve(ctx context.Context, job Job) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	job = NormalizeJob(job)
	if s.BatchSize != 0 {
		job.BatchSize = s.BatchSize
	}
	if job.BatchSize > DefaultBatchSize {
		job.BatchSize = DefaultBatchSize
	}
	if err := checkDifficulty(job.Difficulty); err != nil {
		return Result{}, err
	}

	limit := job.Limit
	if limit == 0 {
		limit = uint64(NotFound) - uint64(job.StartNonce) + 1
	}

	var found C.uint32_t
	var tries C.uint64_t
	deviceName := make([]byte, 256)
	errBuf := make([]byte, 4096)
	rc := C.shasolve_cuda_prepare(
		(*C.char)(unsafe.Pointer(&deviceName[0])),
		C.int(len(deviceName)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	)
	if rc != 0 {
		return Result{}, fmt.Errorf("cuda prepare failed: %s", cudaCString(errBuf))
	}

	started := time.Now()
	rc = C.shasolve_cuda_solve(
		(*C.uint8_t)(unsafe.Pointer(&job.Header[0])),
		C.uint32_t(job.StartNonce),
		C.uint64_t(limit),
		C.uint32_t(job.BatchSize),
		C.uint32_t(job.Difficulty),
		&found,
		&tries,
		(*C.char)(unsafe.Pointer(&deviceName[0])),
		C.int(len(deviceName)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	)
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if rc != 0 {
		return Result{}, fmt.Errorf("cuda solve failed: %s", cudaCString(errBuf))
	}
	if uint32(found) == NotFound {
		return Result{}, fmt.Errorf("no solution in %d hashes", uint64(tries))
	}
	return result("cuda", cudaCString(deviceName), job, uint32(found), uint64(tries), started), nil
}

func cudaCString(buf []byte) string {
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n])
}
