//go:build cuda && linux && cgo

#ifndef SHASOLVE_CUDA_IMPL_H
#define SHASOLVE_CUDA_IMPL_H

#include <stdint.h>

int shasolve_cuda_prepare(char *device_name, int device_name_len, char *errbuf, int errbuf_len);

int shasolve_cuda_solve(
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

#endif
