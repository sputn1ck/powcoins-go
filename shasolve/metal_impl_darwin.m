#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

static NSString *const shaderSource =
@"#include <metal_stdlib>\n"
"using namespace metal;\n"
"struct Params { uint start_nonce; uint count; uint difficulty; uint pad; };\n"
"constant uint K[64] = {\n"
"0x428a2f98u,0x71374491u,0xb5c0fbcfu,0xe9b5dba5u,0x3956c25bu,0x59f111f1u,0x923f82a4u,0xab1c5ed5u,\n"
"0xd807aa98u,0x12835b01u,0x243185beu,0x550c7dc3u,0x72be5d74u,0x80deb1feu,0x9bdc06a7u,0xc19bf174u,\n"
"0xe49b69c1u,0xefbe4786u,0x0fc19dc6u,0x240ca1ccu,0x2de92c6fu,0x4a7484aau,0x5cb0a9dcu,0x76f988dau,\n"
"0x983e5152u,0xa831c66du,0xb00327c8u,0xbf597fc7u,0xc6e00bf3u,0xd5a79147u,0x06ca6351u,0x14292967u,\n"
"0x27b70a85u,0x2e1b2138u,0x4d2c6dfcu,0x53380d13u,0x650a7354u,0x766a0abbu,0x81c2c92eu,0x92722c85u,\n"
"0xa2bfe8a1u,0xa81a664bu,0xc24b8b70u,0xc76c51a3u,0xd192e819u,0xd6990624u,0xf40e3585u,0x106aa070u,\n"
"0x19a4c116u,0x1e376c08u,0x2748774cu,0x34b0bcb5u,0x391c0cb3u,0x4ed8aa4au,0x5b9cca4fu,0x682e6ff3u,\n"
"0x748f82eeu,0x78a5636fu,0x84c87814u,0x8cc70208u,0x90befffau,0xa4506cebu,0xbef9a3f7u,0xc67178f2u};\n"
"struct State { uint mid[8]; uint tail[3]; uint pad; };\n"
"inline uint rotr(uint x, uint n) { return (x >> n) | (x << (32u - n)); }\n"
"inline uint ch(uint x, uint y, uint z) { return (x & y) ^ ((~x) & z); }\n"
"inline uint maj(uint x, uint y, uint z) { return (x & y) ^ (x & z) ^ (y & z); }\n"
"inline uint bsig0(uint x) { return rotr(x, 2u) ^ rotr(x, 13u) ^ rotr(x, 22u); }\n"
"inline uint bsig1(uint x) { return rotr(x, 6u) ^ rotr(x, 11u) ^ rotr(x, 25u); }\n"
"inline uint ssig0(uint x) { return rotr(x, 7u) ^ rotr(x, 18u) ^ (x >> 3u); }\n"
"inline uint ssig1(uint x) { return rotr(x, 17u) ^ rotr(x, 19u) ^ (x >> 10u); }\n"
"inline uint nextw(thread uint w[16], uint i) { uint v=ssig1(w[(i-2u)&15u])+w[(i-7u)&15u]+ssig0(w[(i-15u)&15u])+w[i&15u]; w[i&15u]=v; return v; }\n"
"void compress(thread uint h[8], thread uint w[16]) {\n"
"  uint a=h[0],b=h[1],c=h[2],d=h[3],e=h[4],f=h[5],g=h[6],hh=h[7];\n"
"  #pragma unroll 64\n"
"  for (uint i=0u;i<64u;i++) { uint wi=(i<16u)?w[i]:nextw(w,i); uint t1=hh+bsig1(e)+ch(e,f,g)+K[i]+wi; uint t2=bsig0(a)+maj(a,b,c); hh=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2; }\n"
"  h[0]+=a; h[1]+=b; h[2]+=c; h[3]+=d; h[4]+=e; h[5]+=f; h[6]+=g; h[7]+=hh;\n"
"}\n"
"void init_hash(thread uint h[8]) { h[0]=0x6a09e667u; h[1]=0xbb67ae85u; h[2]=0x3c6ef372u; h[3]=0xa54ff53au; h[4]=0x510e527fu; h[5]=0x9b05688cu; h[6]=0x1f83d9abu; h[7]=0x5be0cd19u; }\n"
"inline bool word_trailing_zero(uint word, uint n) { if (n==0u) return true; if (n>=32u) return word==0u; return (word & ((1u << n)-1u)) == 0u; }\n"
"bool has_trailing_zero_bits(thread uint h[8], uint difficulty) { uint r=difficulty; if (r<=32u) return word_trailing_zero(h[7],r); if (h[7]!=0u) return false; r-=32u; if (r<=32u) return word_trailing_zero(h[6],r); if (h[6]!=0u) return false; r-=32u; return word_trailing_zero(h[5],r); }\n"
"bool check_nonce(constant State &state, uint nonce, uint difficulty) {\n"
"  uint h[8]; uint w[16];\n"
"  for(uint i=0u;i<8u;i++){h[i]=state.mid[i];}\n"
"  w[0]=state.tail[0]; w[1]=state.tail[1]; w[2]=state.tail[2]; w[3]=((nonce&0xffu)<<24u)|(((nonce>>8u)&0xffu)<<16u)|(((nonce>>16u)&0xffu)<<8u)|((nonce>>24u)&0xffu);\n"
"  w[4]=0x80000000u; w[5]=0u; w[6]=0u; w[7]=0u; w[8]=0u; w[9]=0u; w[10]=0u; w[11]=0u; w[12]=0u; w[13]=0u; w[14]=0u; w[15]=640u; compress(h,w);\n"
"  uint h2[8]; init_hash(h2); w[0]=h[0]; w[1]=h[1]; w[2]=h[2]; w[3]=h[3]; w[4]=h[4]; w[5]=h[5]; w[6]=h[6]; w[7]=h[7]; w[8]=0x80000000u; w[9]=0u; w[10]=0u; w[11]=0u; w[12]=0u; w[13]=0u; w[14]=0u; w[15]=256u; compress(h2,w);\n"
"  return has_trailing_zero_bits(h2,difficulty);\n"
"}\n"
"kernel void powcoin_grind(constant State &state [[buffer(0)]], constant Params &params [[buffer(1)]], device atomic_uint *result [[buffer(2)]], uint gid [[thread_position_in_grid]]) {\n"
"  if (gid>=params.count) return; if (atomic_load_explicit(result,memory_order_relaxed)!=0xffffffffu) return; uint nonce=params.start_nonce+gid;\n"
"  if (check_nonce(state,nonce,params.difficulty)) { uint expected=0xffffffffu; atomic_compare_exchange_weak_explicit(result,&expected,nonce,memory_order_relaxed,memory_order_relaxed); }\n"
"}\n";

struct Params { uint32_t start_nonce; uint32_t count; uint32_t difficulty; uint32_t pad; };
struct KernelState { uint32_t mid[8]; uint32_t tail[3]; uint32_t pad; };

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

static void powcoin_midstate(const uint8_t *header, struct KernelState *state) {
	state->mid[0] = 0x6a09e667u; state->mid[1] = 0xbb67ae85u; state->mid[2] = 0x3c6ef372u; state->mid[3] = 0xa54ff53au;
	state->mid[4] = 0x510e527fu; state->mid[5] = 0x9b05688cu; state->mid[6] = 0x1f83d9abu; state->mid[7] = 0x5be0cd19u;
	uint32_t w[16];
	for (uint32_t i = 0; i < 16u; i++) w[i] = header_word(header, i);
	cpu_compress(state->mid, w);
	state->tail[0] = header_word(header, 16u);
	state->tail[1] = header_word(header, 17u);
	state->tail[2] = header_word(header, 18u);
	state->pad = 0;
}

static id<MTLDevice> g_device;
static id<MTLComputePipelineState> g_pipeline;
static id<MTLCommandQueue> g_queue;

static void set_error(char *errbuf, int errbuf_len, NSString *msg) {
	if (errbuf_len > 0) snprintf(errbuf, (size_t)errbuf_len, "%s", msg.UTF8String);
}

static int ensure_metal(char *errbuf, int errbuf_len) {
	if (g_pipeline != nil && g_queue != nil) return 0;
	g_device = MTLCreateSystemDefaultDevice();
	if (g_device == nil) { set_error(errbuf, errbuf_len, @"no default Metal device"); return 1; }
	NSError *error = nil;
	MTLCompileOptions *opts = [MTLCompileOptions new];
	if (@available(macOS 13.3, *)) { opts.maxTotalThreadsPerThreadgroup = 256; }
	if (@available(macOS 26.0, *)) {
		opts.languageVersion = MTLLanguageVersion4_0;
		opts.requiredThreadsPerThreadgroup = MTLSizeMake(256, 1, 1);
	}
	id<MTLLibrary> library = [g_device newLibraryWithSource:shaderSource options:opts error:&error];
	if (library == nil) { set_error(errbuf, errbuf_len, error.localizedDescription ?: @"compile Metal shader"); return 1; }
	id<MTLFunction> function = [library newFunctionWithName:@"powcoin_grind"];
	if (function == nil) { set_error(errbuf, errbuf_len, @"missing powcoin_grind kernel"); return 1; }
	g_pipeline = [g_device newComputePipelineStateWithFunction:function error:&error];
	if (g_pipeline == nil) { set_error(errbuf, errbuf_len, error.localizedDescription ?: @"create pipeline"); return 1; }
	g_queue = [g_device newCommandQueue];
	if (g_queue == nil) { set_error(errbuf, errbuf_len, @"create command queue"); return 1; }
	return 0;
}

int shasolve_metal_prepare(char *device_name, int device_name_len, char *errbuf, int errbuf_len) {
	@autoreleasepool {
		if (ensure_metal(errbuf, errbuf_len) != 0) return 1;
		if (device_name_len > 0) snprintf(device_name, (size_t)device_name_len, "%s", g_device.name.UTF8String);
		return 0;
	}
}

int shasolve_metal_solve(const uint8_t *header, uint32_t start_nonce, uint64_t limit, uint32_t batch_size, uint32_t difficulty, uint32_t *found, uint64_t *tries, char *device_name, int device_name_len, char *errbuf, int errbuf_len) {
	@autoreleasepool {
		*found = UINT32_MAX; *tries = 0;
		if (ensure_metal(errbuf, errbuf_len) != 0) return 1;
		if (device_name_len > 0) snprintf(device_name, (size_t)device_name_len, "%s", g_device.name.UTF8String);
		struct KernelState state;
		powcoin_midstate(header, &state);
		id<MTLBuffer> resultBuffer = [g_device newBufferWithLength:sizeof(uint32_t) options:MTLResourceStorageModeShared];
		if (resultBuffer == nil) { set_error(errbuf, errbuf_len, @"allocate Metal buffers"); return 1; }
		uint32_t current = start_nonce; uint64_t remaining = limit;
		while (remaining > 0) {
			uint32_t count = remaining > batch_size ? batch_size : (uint32_t)remaining;
			*(uint32_t *)resultBuffer.contents = UINT32_MAX;
			struct Params params;
			params.start_nonce = current; params.count = count; params.difficulty = difficulty; params.pad = 0;
			id<MTLCommandBuffer> cb = [g_queue commandBuffer];
			id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
			[enc setComputePipelineState:g_pipeline]; [enc setBytes:&state length:sizeof(state) atIndex:0]; [enc setBytes:&params length:sizeof(params) atIndex:1]; [enc setBuffer:resultBuffer offset:0 atIndex:2];
			NSUInteger width = 256;
			NSUInteger maxThreads = g_pipeline.maxTotalThreadsPerThreadgroup;
			if (maxThreads > 0 && width > maxThreads) width = maxThreads;
			NSUInteger executionWidth = g_pipeline.threadExecutionWidth;
			if (executionWidth > 0 && width < executionWidth) width = executionWidth;
			[enc dispatchThreads:MTLSizeMake(count,1,1) threadsPerThreadgroup:MTLSizeMake(width,1,1)];
			[enc endEncoding]; [cb commit]; [cb waitUntilCompleted];
			if (cb.error != nil) { set_error(errbuf, errbuf_len, cb.error.localizedDescription); return 1; }
			*tries += count; uint32_t batch_found = *(uint32_t *)resultBuffer.contents;
			if (batch_found != UINT32_MAX) { *found = batch_found; return 0; }
			if (UINT32_MAX - current < count) break; current += count; remaining -= count;
		}
		return 0;
	}
}
