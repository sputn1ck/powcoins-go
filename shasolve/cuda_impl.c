//go:build cuda && linux && cgo

#include "cuda_impl.h"

#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CUDA_SUCCESS 0
#define NVRTC_SUCCESS 0
#define NOT_FOUND UINT32_MAX

typedef int CUdevice;
typedef int CUresult;
typedef unsigned long long CUdeviceptr;
typedef struct CUctx_st *CUcontext;
typedef struct CUmod_st *CUmodule;
typedef struct CUfunc_st *CUfunction;
typedef struct _nvrtcProgram *nvrtcProgram;
typedef int nvrtcResult;

static void *cuda_lib;
static void *nvrtc_lib;
static CUcontext cuda_ctx;
static CUmodule cuda_module;
static CUfunction cuda_kernel;
static char cuda_device_name[256];
static char cuda_arch_option[64];

static CUresult (*p_cuInit)(unsigned int);
static CUresult (*p_cuDeviceGet)(CUdevice *, int);
static CUresult (*p_cuDeviceGetName)(char *, int, CUdevice);
static CUresult (*p_cuDeviceComputeCapability)(int *, int *, CUdevice);
static CUresult (*p_cuCtxCreate_v2)(CUcontext *, unsigned int, CUdevice);
static CUresult (*p_cuCtxDestroy_v2)(CUcontext);
static CUresult (*p_cuCtxSynchronize)(void);
static CUresult (*p_cuMemAlloc_v2)(CUdeviceptr *, size_t);
static CUresult (*p_cuMemFree_v2)(CUdeviceptr);
static CUresult (*p_cuMemcpyHtoD_v2)(CUdeviceptr, const void *, size_t);
static CUresult (*p_cuMemcpyDtoH_v2)(void *, CUdeviceptr, size_t);
static CUresult (*p_cuModuleLoadDataEx)(CUmodule *, const void *, unsigned int, void *, void *);
static CUresult (*p_cuModuleUnload)(CUmodule);
static CUresult (*p_cuModuleGetFunction)(CUfunction *, CUmodule, const char *);
static CUresult (*p_cuLaunchKernel)(CUfunction, unsigned int, unsigned int, unsigned int, unsigned int, unsigned int, unsigned int, unsigned int, void *, void **, void **);
static CUresult (*p_cuGetErrorString)(CUresult, const char **);

static nvrtcResult (*p_nvrtcCreateProgram)(nvrtcProgram *, const char *, const char *, int, const char *const *, const char *const *);
static nvrtcResult (*p_nvrtcCompileProgram)(nvrtcProgram, int, const char *const *);
static nvrtcResult (*p_nvrtcGetPTXSize)(nvrtcProgram, size_t *);
static nvrtcResult (*p_nvrtcGetPTX)(nvrtcProgram, char *);
static nvrtcResult (*p_nvrtcGetProgramLogSize)(nvrtcProgram, size_t *);
static nvrtcResult (*p_nvrtcGetProgramLog)(nvrtcProgram, char *);
static nvrtcResult (*p_nvrtcDestroyProgram)(nvrtcProgram *);
static const char *(*p_nvrtcGetErrorString)(nvrtcResult);

static const char *cuda_kernel_source =
"extern \"C\" {\n"
"typedef unsigned int uint;\n"
"__device__ __constant__ uint K[64] = {\n"
"0x428a2f98u,0x71374491u,0xb5c0fbcfu,0xe9b5dba5u,0x3956c25bu,0x59f111f1u,0x923f82a4u,0xab1c5ed5u,\n"
"0xd807aa98u,0x12835b01u,0x243185beu,0x550c7dc3u,0x72be5d74u,0x80deb1feu,0x9bdc06a7u,0xc19bf174u,\n"
"0xe49b69c1u,0xefbe4786u,0x0fc19dc6u,0x240ca1ccu,0x2de92c6fu,0x4a7484aau,0x5cb0a9dcu,0x76f988dau,\n"
"0x983e5152u,0xa831c66du,0xb00327c8u,0xbf597fc7u,0xc6e00bf3u,0xd5a79147u,0x06ca6351u,0x14292967u,\n"
"0x27b70a85u,0x2e1b2138u,0x4d2c6dfcu,0x53380d13u,0x650a7354u,0x766a0abbu,0x81c2c92eu,0x92722c85u,\n"
"0xa2bfe8a1u,0xa81a664bu,0xc24b8b70u,0xc76c51a3u,0xd192e819u,0xd6990624u,0xf40e3585u,0x106aa070u,\n"
"0x19a4c116u,0x1e376c08u,0x2748774cu,0x34b0bcb5u,0x391c0cb3u,0x4ed8aa4au,0x5b9cca4fu,0x682e6ff3u,\n"
"0x748f82eeu,0x78a5636fu,0x84c87814u,0x8cc70208u,0x90befffau,0xa4506cebu,0xbef9a3f7u,0xc67178f2u};\n"
"__device__ uint rotr(uint x, uint n) { return (x >> n) | (x << (32u - n)); }\n"
"__device__ uint ch(uint x, uint y, uint z) { return (x & y) ^ ((~x) & z); }\n"
"__device__ uint maj(uint x, uint y, uint z) { return (x & y) ^ (x & z) ^ (y & z); }\n"
"__device__ uint bsig0(uint x) { return rotr(x, 2u) ^ rotr(x, 13u) ^ rotr(x, 22u); }\n"
"__device__ uint bsig1(uint x) { return rotr(x, 6u) ^ rotr(x, 11u) ^ rotr(x, 25u); }\n"
"__device__ uint ssig0(uint x) { return rotr(x, 7u) ^ rotr(x, 18u) ^ (x >> 3u); }\n"
"__device__ uint ssig1(uint x) { return rotr(x, 17u) ^ rotr(x, 19u) ^ (x >> 10u); }\n"
"__device__ uint word_at(const unsigned char *header, uint idx) { uint j=idx*4u; return (uint(header[j])<<24u)|(uint(header[j+1u])<<16u)|(uint(header[j+2u])<<8u)|uint(header[j+3u]); }\n"
"__device__ void compress(uint h[8], uint w[64]) {\n"
"  for (uint i=16u;i<64u;i++) { w[i]=ssig1(w[i-2u])+w[i-7u]+ssig0(w[i-15u])+w[i-16u]; }\n"
"  uint a=h[0],b=h[1],c=h[2],d=h[3],e=h[4],f=h[5],g=h[6],hh=h[7];\n"
"  for (uint i=0u;i<64u;i++) { uint t1=hh+bsig1(e)+ch(e,f,g)+K[i]+w[i]; uint t2=bsig0(a)+maj(a,b,c); hh=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2; }\n"
"  h[0]+=a; h[1]+=b; h[2]+=c; h[3]+=d; h[4]+=e; h[5]+=f; h[6]+=g; h[7]+=hh;\n"
"}\n"
"__device__ void init_hash(uint h[8]) { h[0]=0x6a09e667u; h[1]=0xbb67ae85u; h[2]=0x3c6ef372u; h[3]=0xa54ff53au; h[4]=0x510e527fu; h[5]=0x9b05688cu; h[6]=0x1f83d9abu; h[7]=0x5be0cd19u; }\n"
"__device__ bool word_trailing_zero(uint word, uint n) { if (n==0u) return true; if (n>=32u) return word==0u; return (word & ((1u << n)-1u)) == 0u; }\n"
"__device__ bool has_trailing_zero_bits(uint h[8], uint difficulty) { uint r=difficulty; if (r<=32u) return word_trailing_zero(h[7],r); if (h[7]!=0u) return false; r-=32u; if (r<=32u) return word_trailing_zero(h[6],r); if (h[6]!=0u) return false; r-=32u; return word_trailing_zero(h[5],r); }\n"
"__device__ bool check_nonce(const unsigned char *header, uint nonce, uint difficulty) {\n"
"  uint h[8]; uint w[64]; init_hash(h);\n"
"  for (uint i=0u;i<16u;i++) { w[i]=word_at(header,i); }\n"
"  compress(h,w);\n"
"  w[0]=word_at(header,16u); w[1]=word_at(header,17u); w[2]=word_at(header,18u); w[3]=((nonce&0xffu)<<24u)|(((nonce>>8u)&0xffu)<<16u)|(((nonce>>16u)&0xffu)<<8u)|((nonce>>24u)&0xffu);\n"
"  w[4]=0x80000000u; for(uint i=5u;i<15u;i++){w[i]=0u;} w[15]=640u; compress(h,w);\n"
"  uint h2[8]; init_hash(h2); for(uint i=0u;i<8u;i++){w[i]=h[i];} w[8]=0x80000000u; for(uint i=9u;i<15u;i++){w[i]=0u;} w[15]=256u; compress(h2,w);\n"
"  return has_trailing_zero_bits(h2,difficulty);\n"
"}\n"
"__global__ void powcoin_grind(const unsigned char *header, uint start_nonce, uint count, uint difficulty, uint *result) {\n"
"  uint gid = blockIdx.x * blockDim.x + threadIdx.x;\n"
"  if (gid>=count) return;\n"
"  if (*result != 0xffffffffu) return;\n"
"  uint nonce=start_nonce+gid;\n"
"  if (check_nonce(header,nonce,difficulty)) { atomicCAS(result,0xffffffffu,nonce); }\n"
"}\n"
"}\n";

static const char *cuda_kernel_source_optimized =
"extern \"C\" {\n"
"typedef unsigned int uint;\n"
"__device__ __constant__ uint K[64] = {\n"
"0x428a2f98u,0x71374491u,0xb5c0fbcfu,0xe9b5dba5u,0x3956c25bu,0x59f111f1u,0x923f82a4u,0xab1c5ed5u,\n"
"0xd807aa98u,0x12835b01u,0x243185beu,0x550c7dc3u,0x72be5d74u,0x80deb1feu,0x9bdc06a7u,0xc19bf174u,\n"
"0xe49b69c1u,0xefbe4786u,0x0fc19dc6u,0x240ca1ccu,0x2de92c6fu,0x4a7484aau,0x5cb0a9dcu,0x76f988dau,\n"
"0x983e5152u,0xa831c66du,0xb00327c8u,0xbf597fc7u,0xc6e00bf3u,0xd5a79147u,0x06ca6351u,0x14292967u,\n"
"0x27b70a85u,0x2e1b2138u,0x4d2c6dfcu,0x53380d13u,0x650a7354u,0x766a0abbu,0x81c2c92eu,0x92722c85u,\n"
"0xa2bfe8a1u,0xa81a664bu,0xc24b8b70u,0xc76c51a3u,0xd192e819u,0xd6990624u,0xf40e3585u,0x106aa070u,\n"
"0x19a4c116u,0x1e376c08u,0x2748774cu,0x34b0bcb5u,0x391c0cb3u,0x4ed8aa4au,0x5b9cca4fu,0x682e6ff3u,\n"
"0x748f82eeu,0x78a5636fu,0x84c87814u,0x8cc70208u,0x90befffau,0xa4506cebu,0xbef9a3f7u,0xc67178f2u};\n"
"__device__ __forceinline__ uint rotr(uint x, uint n) { return (x >> n) | (x << (32u - n)); }\n"
"__device__ __forceinline__ uint ch(uint x, uint y, uint z) { return (x & y) ^ ((~x) & z); }\n"
"__device__ __forceinline__ uint maj(uint x, uint y, uint z) { return (x & y) ^ (x & z) ^ (y & z); }\n"
"__device__ __forceinline__ uint bsig0(uint x) { return rotr(x, 2u) ^ rotr(x, 13u) ^ rotr(x, 22u); }\n"
"__device__ __forceinline__ uint bsig1(uint x) { return rotr(x, 6u) ^ rotr(x, 11u) ^ rotr(x, 25u); }\n"
"__device__ __forceinline__ uint ssig0(uint x) { return rotr(x, 7u) ^ rotr(x, 18u) ^ (x >> 3u); }\n"
"__device__ __forceinline__ uint ssig1(uint x) { return rotr(x, 17u) ^ rotr(x, 19u) ^ (x >> 10u); }\n"
"__device__ __forceinline__ uint nextw(uint w[16], uint i) { uint v=ssig1(w[(i-2u)&15u])+w[(i-7u)&15u]+ssig0(w[(i-15u)&15u])+w[i&15u]; w[i&15u]=v; return v; }\n"
"__device__ __forceinline__ void init_hash(uint h[8]) { h[0]=0x6a09e667u; h[1]=0xbb67ae85u; h[2]=0x3c6ef372u; h[3]=0xa54ff53au; h[4]=0x510e527fu; h[5]=0x9b05688cu; h[6]=0x1f83d9abu; h[7]=0x5be0cd19u; }\n"
"__device__ void compress(uint h[8], uint w[16]) {\n"
"  uint a=h[0],b=h[1],c=h[2],d=h[3],e=h[4],f=h[5],g=h[6],hh=h[7];\n"
"  #pragma unroll 64\n"
"  for (uint i=0u;i<64u;i++) { uint wi=(i<16u)?w[i]:nextw(w,i); uint t1=hh+bsig1(e)+ch(e,f,g)+K[i]+wi; uint t2=bsig0(a)+maj(a,b,c); hh=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2; }\n"
"  h[0]+=a; h[1]+=b; h[2]+=c; h[3]+=d; h[4]+=e; h[5]+=f; h[6]+=g; h[7]+=hh;\n"
"}\n"
"__device__ __forceinline__ bool word_trailing_zero(uint word, uint n) { if (n==0u) return true; if (n>=32u) return word==0u; return (word & ((1u << n)-1u)) == 0u; }\n"
"__device__ __forceinline__ bool has_trailing_zero_bits(uint h[8], uint difficulty) { uint r=difficulty; if (r<=32u) return word_trailing_zero(h[7],r); if (h[7]!=0u) return false; r-=32u; if (r<=32u) return word_trailing_zero(h[6],r); if (h[6]!=0u) return false; r-=32u; return word_trailing_zero(h[5],r); }\n"
"__device__ bool check_nonce(const uint *mid, uint w0, uint w1, uint w2, uint nonce, uint difficulty) {\n"
"  uint h[8]; uint w[16];\n"
"  #pragma unroll\n"
"  for(uint i=0u;i<8u;i++){h[i]=mid[i];}\n"
"  w[0]=w0; w[1]=w1; w[2]=w2; w[3]=((nonce&0xffu)<<24u)|(((nonce>>8u)&0xffu)<<16u)|(((nonce>>16u)&0xffu)<<8u)|((nonce>>24u)&0xffu);\n"
"  w[4]=0x80000000u; w[5]=0u; w[6]=0u; w[7]=0u; w[8]=0u; w[9]=0u; w[10]=0u; w[11]=0u; w[12]=0u; w[13]=0u; w[14]=0u; w[15]=640u; compress(h,w);\n"
"  uint h2[8]; init_hash(h2); w[0]=h[0]; w[1]=h[1]; w[2]=h[2]; w[3]=h[3]; w[4]=h[4]; w[5]=h[5]; w[6]=h[6]; w[7]=h[7]; w[8]=0x80000000u; w[9]=0u; w[10]=0u; w[11]=0u; w[12]=0u; w[13]=0u; w[14]=0u; w[15]=256u; compress(h2,w);\n"
"  return has_trailing_zero_bits(h2,difficulty);\n"
"}\n"
"__global__ void powcoin_grind(const uint *mid, uint w0, uint w1, uint w2, uint start_nonce, uint count, uint difficulty, uint *result) {\n"
"  uint gid = blockIdx.x * blockDim.x + threadIdx.x;\n"
"  if (gid>=count) return;\n"
"  if (*result != 0xffffffffu) return;\n"
"  uint nonce=start_nonce+gid;\n"
"  if (check_nonce(mid,w0,w1,w2,nonce,difficulty)) { atomicCAS(result,0xffffffffu,nonce); }\n"
"}\n"
"}\n";

static const uint32_t cpu_k[64] = {
	0x428a2f98u, 0x71374491u, 0xb5c0fbcfu, 0xe9b5dba5u, 0x3956c25bu, 0x59f111f1u, 0x923f82a4u, 0xab1c5ed5u,
	0xd807aa98u, 0x12835b01u, 0x243185beu, 0x550c7dc3u, 0x72be5d74u, 0x80deb1feu, 0x9bdc06a7u, 0xc19bf174u,
	0xe49b69c1u, 0xefbe4786u, 0x0fc19dc6u, 0x240ca1ccu, 0x2de92c6fu, 0x4a7484aau, 0x5cb0a9dcu, 0x76f988dau,
	0x983e5152u, 0xa831c66du, 0xb00327c8u, 0xbf597fc7u, 0xc6e00bf3u, 0xd5a79147u, 0x06ca6351u, 0x14292967u,
	0x27b70a85u, 0x2e1b2138u, 0x4d2c6dfcu, 0x53380d13u, 0x650a7354u, 0x766a0abbu, 0x81c2c92eu, 0x92722c85u,
	0xa2bfe8a1u, 0xa81a664bu, 0xc24b8b70u, 0xc76c51a3u, 0xd192e819u, 0xd6990624u, 0xf40e3585u, 0x106aa070u,
	0x19a4c116u, 0x1e376c08u, 0x2748774cu, 0x34b0bcb5u, 0x391c0cb3u, 0x4ed8aa4au, 0x5b9cca4fu, 0x682e6ff3u,
	0x748f82eeu, 0x78a5636fu, 0x84c87814u, 0x8cc70208u, 0x90befffau, 0xa4506cebu, 0xbef9a3f7u, 0xc67178f2u,
};

static uint32_t rotr32(uint32_t x, uint32_t n) { return (x >> n) | (x << (32u - n)); }
static uint32_t ch32(uint32_t x, uint32_t y, uint32_t z) { return (x & y) ^ ((~x) & z); }
static uint32_t maj32(uint32_t x, uint32_t y, uint32_t z) { return (x & y) ^ (x & z) ^ (y & z); }
static uint32_t bsig0_32(uint32_t x) { return rotr32(x, 2u) ^ rotr32(x, 13u) ^ rotr32(x, 22u); }
static uint32_t bsig1_32(uint32_t x) { return rotr32(x, 6u) ^ rotr32(x, 11u) ^ rotr32(x, 25u); }
static uint32_t ssig0_32(uint32_t x) { return rotr32(x, 7u) ^ rotr32(x, 18u) ^ (x >> 3u); }
static uint32_t ssig1_32(uint32_t x) { return rotr32(x, 17u) ^ rotr32(x, 19u) ^ (x >> 10u); }

static uint32_t header_word(const uint8_t *header, uint32_t idx) {
	uint32_t j = idx * 4u;
	return ((uint32_t)header[j] << 24u) | ((uint32_t)header[j + 1u] << 16u) | ((uint32_t)header[j + 2u] << 8u) | (uint32_t)header[j + 3u];
}

static void cpu_compress(uint32_t h[8], uint32_t w[16]) {
	uint32_t a = h[0], b = h[1], c = h[2], d = h[3], e = h[4], f = h[5], g = h[6], hh = h[7];
	for (uint32_t i = 0; i < 64u; i++) {
		uint32_t wi = i < 16u ? w[i] : (w[i & 15u] = ssig1_32(w[(i - 2u) & 15u]) + w[(i - 7u) & 15u] + ssig0_32(w[(i - 15u) & 15u]) + w[i & 15u]);
		uint32_t t1 = hh + bsig1_32(e) + ch32(e, f, g) + cpu_k[i] + wi;
		uint32_t t2 = bsig0_32(a) + maj32(a, b, c);
		hh = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
	}
	h[0] += a; h[1] += b; h[2] += c; h[3] += d; h[4] += e; h[5] += f; h[6] += g; h[7] += hh;
}

static void powcoin_midstate(const uint8_t *header, uint32_t mid[8], uint32_t tail_words[3]) {
	mid[0] = 0x6a09e667u; mid[1] = 0xbb67ae85u; mid[2] = 0x3c6ef372u; mid[3] = 0xa54ff53au;
	mid[4] = 0x510e527fu; mid[5] = 0x9b05688cu; mid[6] = 0x1f83d9abu; mid[7] = 0x5be0cd19u;
	uint32_t w[16];
	for (uint32_t i = 0; i < 16u; i++) w[i] = header_word(header, i);
	cpu_compress(mid, w);
	tail_words[0] = header_word(header, 16u);
	tail_words[1] = header_word(header, 17u);
	tail_words[2] = header_word(header, 18u);
}

static void set_error(char *errbuf, int errbuf_len, const char *msg) {
	if (errbuf_len <= 0) return;
	snprintf(errbuf, (size_t)errbuf_len, "%s", msg ? msg : "unknown error");
}

static void set_cuda_error(char *errbuf, int errbuf_len, const char *op, CUresult rc) {
	const char *detail = NULL;
	if (p_cuGetErrorString) p_cuGetErrorString(rc, &detail);
	if (!detail) detail = "unknown CUDA error";
	if (errbuf_len > 0) snprintf(errbuf, (size_t)errbuf_len, "%s: %s (%d)", op, detail, rc);
}

static void *load_symbol(void *lib, const char *name, char *errbuf, int errbuf_len) {
	void *sym = dlsym(lib, name);
	if (!sym && errbuf_len > 0) snprintf(errbuf, (size_t)errbuf_len, "missing symbol %s", name);
	return sym;
}

#define LOAD(lib, name) do { p_##name = load_symbol((lib), #name, errbuf, errbuf_len); if (!p_##name) return 1; } while (0)

static int load_libraries(char *errbuf, int errbuf_len) {
	if (cuda_lib && nvrtc_lib) return 0;
	cuda_lib = dlopen("libcuda.so.1", RTLD_NOW | RTLD_LOCAL);
	if (!cuda_lib) cuda_lib = dlopen("libcuda.so", RTLD_NOW | RTLD_LOCAL);
	if (!cuda_lib) {
		set_error(errbuf, errbuf_len, dlerror());
		return 1;
	}
	nvrtc_lib = dlopen("libnvrtc.so", RTLD_NOW | RTLD_LOCAL);
	if (!nvrtc_lib) {
		set_error(errbuf, errbuf_len, dlerror());
		return 1;
	}

	LOAD(cuda_lib, cuInit);
	LOAD(cuda_lib, cuDeviceGet);
	LOAD(cuda_lib, cuDeviceGetName);
	LOAD(cuda_lib, cuDeviceComputeCapability);
	LOAD(cuda_lib, cuCtxCreate_v2);
	LOAD(cuda_lib, cuCtxDestroy_v2);
	LOAD(cuda_lib, cuCtxSynchronize);
	LOAD(cuda_lib, cuMemAlloc_v2);
	LOAD(cuda_lib, cuMemFree_v2);
	LOAD(cuda_lib, cuMemcpyHtoD_v2);
	LOAD(cuda_lib, cuMemcpyDtoH_v2);
	LOAD(cuda_lib, cuModuleLoadDataEx);
	LOAD(cuda_lib, cuModuleUnload);
	LOAD(cuda_lib, cuModuleGetFunction);
	LOAD(cuda_lib, cuLaunchKernel);
	LOAD(cuda_lib, cuGetErrorString);

	LOAD(nvrtc_lib, nvrtcCreateProgram);
	LOAD(nvrtc_lib, nvrtcCompileProgram);
	LOAD(nvrtc_lib, nvrtcGetPTXSize);
	LOAD(nvrtc_lib, nvrtcGetPTX);
	LOAD(nvrtc_lib, nvrtcGetProgramLogSize);
	LOAD(nvrtc_lib, nvrtcGetProgramLog);
	LOAD(nvrtc_lib, nvrtcDestroyProgram);
	LOAD(nvrtc_lib, nvrtcGetErrorString);
	return 0;
}

static int compile_kernel(char *errbuf, int errbuf_len) {
	if (cuda_module && cuda_kernel) return 0;

	nvrtcProgram prog = NULL;
	nvrtcResult nrc = p_nvrtcCreateProgram(&prog, cuda_kernel_source_optimized, "powcoin_grind.cu", 0, NULL, NULL);
	if (nrc != NVRTC_SUCCESS) {
		set_error(errbuf, errbuf_len, p_nvrtcGetErrorString(nrc));
		return 1;
	}

	const char *arch = cuda_arch_option[0] != 0 ? cuda_arch_option : "--gpu-architecture=compute_52";
	const char *opts[] = {arch, "--std=c++11"};
	nrc = p_nvrtcCompileProgram(prog, 2, opts);
	if (nrc != NVRTC_SUCCESS) {
		size_t log_size = 0;
		p_nvrtcGetProgramLogSize(prog, &log_size);
		if (log_size > 1) {
			char *log = (char *)malloc(log_size);
			if (log) {
				p_nvrtcGetProgramLog(prog, log);
				set_error(errbuf, errbuf_len, log);
				free(log);
			} else {
				set_error(errbuf, errbuf_len, p_nvrtcGetErrorString(nrc));
			}
		} else {
			set_error(errbuf, errbuf_len, p_nvrtcGetErrorString(nrc));
		}
		p_nvrtcDestroyProgram(&prog);
		return 1;
	}

	size_t ptx_size = 0;
	nrc = p_nvrtcGetPTXSize(prog, &ptx_size);
	if (nrc != NVRTC_SUCCESS) {
		set_error(errbuf, errbuf_len, p_nvrtcGetErrorString(nrc));
		p_nvrtcDestroyProgram(&prog);
		return 1;
	}
	char *ptx = (char *)malloc(ptx_size);
	if (!ptx) {
		set_error(errbuf, errbuf_len, "allocate PTX buffer");
		p_nvrtcDestroyProgram(&prog);
		return 1;
	}
	nrc = p_nvrtcGetPTX(prog, ptx);
	p_nvrtcDestroyProgram(&prog);
	if (nrc != NVRTC_SUCCESS) {
		set_error(errbuf, errbuf_len, p_nvrtcGetErrorString(nrc));
		free(ptx);
		return 1;
	}

	CUresult crc = p_cuModuleLoadDataEx(&cuda_module, ptx, 0, NULL, NULL);
	free(ptx);
	if (crc != CUDA_SUCCESS) {
		set_cuda_error(errbuf, errbuf_len, "cuModuleLoadDataEx", crc);
		return 1;
	}
	crc = p_cuModuleGetFunction(&cuda_kernel, cuda_module, "powcoin_grind");
	if (crc != CUDA_SUCCESS) {
		set_cuda_error(errbuf, errbuf_len, "cuModuleGetFunction", crc);
		p_cuModuleUnload(cuda_module);
		cuda_module = NULL;
		return 1;
	}
	return 0;
}

static int ensure_cuda(char *errbuf, int errbuf_len) {
	if (load_libraries(errbuf, errbuf_len) != 0) return 1;
	CUresult rc = p_cuInit(0);
	if (rc != CUDA_SUCCESS) {
		set_cuda_error(errbuf, errbuf_len, "cuInit", rc);
		return 1;
	}
	CUdevice dev = 0;
	rc = p_cuDeviceGet(&dev, 0);
	if (rc != CUDA_SUCCESS) {
		set_cuda_error(errbuf, errbuf_len, "cuDeviceGet", rc);
		return 1;
	}
	if (cuda_device_name[0] == 0) {
		rc = p_cuDeviceGetName(cuda_device_name, (int)sizeof(cuda_device_name), dev);
		if (rc != CUDA_SUCCESS) snprintf(cuda_device_name, sizeof(cuda_device_name), "CUDA device 0");
	}
	if (cuda_arch_option[0] == 0) {
		int major = 5;
		int minor = 2;
		rc = p_cuDeviceComputeCapability(&major, &minor, dev);
		if (rc == CUDA_SUCCESS) {
			snprintf(cuda_arch_option, sizeof(cuda_arch_option), "--gpu-architecture=compute_%d%d", major, minor);
		} else {
			snprintf(cuda_arch_option, sizeof(cuda_arch_option), "--gpu-architecture=compute_52");
		}
	}
	if (!cuda_ctx) {
		rc = p_cuCtxCreate_v2(&cuda_ctx, 0, dev);
		if (rc != CUDA_SUCCESS) {
			set_cuda_error(errbuf, errbuf_len, "cuCtxCreate", rc);
			return 1;
		}
	}
	return compile_kernel(errbuf, errbuf_len);
}

int shasolve_cuda_prepare(char *device_name, int device_name_len, char *errbuf, int errbuf_len) {
	if (ensure_cuda(errbuf, errbuf_len) != 0) return 1;
	if (device_name_len > 0) snprintf(device_name, (size_t)device_name_len, "%s", cuda_device_name);
	return 0;
}

int shasolve_cuda_solve(const uint8_t *header, uint32_t start_nonce, uint64_t limit, uint32_t batch_size, uint32_t difficulty, uint32_t *found, uint64_t *tries, char *device_name, int device_name_len, char *errbuf, int errbuf_len) {
	*found = NOT_FOUND;
	*tries = 0;
	if (ensure_cuda(errbuf, errbuf_len) != 0) return 1;
	if (device_name_len > 0) snprintf(device_name, (size_t)device_name_len, "%s", cuda_device_name);

	uint32_t mid[8];
	uint32_t tail_words[3];
	powcoin_midstate(header, mid, tail_words);

	CUdeviceptr d_mid = 0;
	CUdeviceptr d_result = 0;
	CUresult rc = p_cuMemAlloc_v2(&d_mid, sizeof(mid));
	if (rc != CUDA_SUCCESS) {
		set_cuda_error(errbuf, errbuf_len, "cuMemAlloc midstate", rc);
		return 1;
	}
	rc = p_cuMemAlloc_v2(&d_result, 4);
	if (rc != CUDA_SUCCESS) {
		p_cuMemFree_v2(d_mid);
		set_cuda_error(errbuf, errbuf_len, "cuMemAlloc result", rc);
		return 1;
	}
	rc = p_cuMemcpyHtoD_v2(d_mid, mid, sizeof(mid));
	if (rc != CUDA_SUCCESS) {
		p_cuMemFree_v2(d_result);
		p_cuMemFree_v2(d_mid);
		set_cuda_error(errbuf, errbuf_len, "cuMemcpyHtoD midstate", rc);
		return 1;
	}

	uint32_t current = start_nonce;
	uint64_t remaining = limit;
	while (remaining > 0) {
		uint32_t count = remaining > batch_size ? batch_size : (uint32_t)remaining;
		if (count == 0) break;
		uint32_t not_found = NOT_FOUND;
		rc = p_cuMemcpyHtoD_v2(d_result, &not_found, 4);
		if (rc != CUDA_SUCCESS) {
			set_cuda_error(errbuf, errbuf_len, "cuMemcpyHtoD result", rc);
			break;
		}

		void *args[] = {&d_mid, &tail_words[0], &tail_words[1], &tail_words[2], &current, &count, &difficulty, &d_result};
		unsigned int threads = 256;
		unsigned int blocks = (count + threads - 1) / threads;
		rc = p_cuLaunchKernel(cuda_kernel, blocks, 1, 1, threads, 1, 1, 0, NULL, args, NULL);
		if (rc != CUDA_SUCCESS) {
			set_cuda_error(errbuf, errbuf_len, "cuLaunchKernel", rc);
			break;
		}
		rc = p_cuCtxSynchronize();
		if (rc != CUDA_SUCCESS) {
			set_cuda_error(errbuf, errbuf_len, "cuCtxSynchronize", rc);
			break;
		}
		uint32_t batch_found = NOT_FOUND;
		rc = p_cuMemcpyDtoH_v2(&batch_found, d_result, 4);
		if (rc != CUDA_SUCCESS) {
			set_cuda_error(errbuf, errbuf_len, "cuMemcpyDtoH result", rc);
			break;
		}

		*tries += count;
		if (batch_found != NOT_FOUND) {
			*found = batch_found;
			p_cuMemFree_v2(d_result);
			p_cuMemFree_v2(d_mid);
			return 0;
		}
		if (UINT32_MAX - current < count) break;
		current += count;
		remaining -= count;
	}

	p_cuMemFree_v2(d_result);
	p_cuMemFree_v2(d_mid);
	return *found == NOT_FOUND ? 0 : 0;
}
