//go:build darwin && cgo

package shasolve

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework Metal
#include <stdint.h>
#include <stdlib.h>

int shasolve_metal_solve(
	const uint8_t *header,
	uint32_t start_nonce,
	uint64_t limit,
	uint32_t batch_size,
	uint32_t difficulty,
	uint32_t *found,
	uint64_t *tries,
	char *device_name,
	int device_name_len,
	char *errbuf,
	int errbuf_len
);

int shasolve_metal_prepare(
	char *device_name,
	int device_name_len,
	char *errbuf,
	int errbuf_len
);

*/
import "C"

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

type MetalSolver struct {
	BatchSize uint32
}

func (MetalSolver) Name() string { return "metal" }

func (s MetalSolver) Solve(ctx context.Context, job Job) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	job = NormalizeJob(job)
	if s.BatchSize != 0 {
		job.BatchSize = s.BatchSize
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
	errBuf := make([]byte, 1024)
	rc := C.shasolve_metal_prepare(
		(*C.char)(unsafe.Pointer(&deviceName[0])),
		C.int(len(deviceName)),
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.int(len(errBuf)),
	)
	if rc != 0 {
		return Result{}, fmt.Errorf("metal prepare failed: %s", cString(errBuf))
	}
	started := time.Now()
	rc = C.shasolve_metal_solve(
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
		return Result{}, fmt.Errorf("metal solve failed: %s", cString(errBuf))
	}
	if uint32(found) == NotFound {
		return Result{}, fmt.Errorf("no solution in %d hashes", uint64(tries))
	}
	return result("metal", cString(deviceName), job, uint32(found), uint64(tries), started), nil
}

func cString(buf []byte) string {
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n])
}
