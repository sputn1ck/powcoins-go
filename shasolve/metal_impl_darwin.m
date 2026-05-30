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
"uint rotr(uint x, uint n) { return (x >> n) | (x << (32u - n)); }\n"
"uint ch(uint x, uint y, uint z) { return (x & y) ^ ((~x) & z); }\n"
"uint maj(uint x, uint y, uint z) { return (x & y) ^ (x & z) ^ (y & z); }\n"
"uint bsig0(uint x) { return rotr(x, 2u) ^ rotr(x, 13u) ^ rotr(x, 22u); }\n"
"uint bsig1(uint x) { return rotr(x, 6u) ^ rotr(x, 11u) ^ rotr(x, 25u); }\n"
"uint ssig0(uint x) { return rotr(x, 7u) ^ rotr(x, 18u) ^ (x >> 3u); }\n"
"uint ssig1(uint x) { return rotr(x, 17u) ^ rotr(x, 19u) ^ (x >> 10u); }\n"
"uint word_at(const device uchar *header, uint idx) { uint j=idx*4u; return (uint(header[j])<<24u)|(uint(header[j+1u])<<16u)|(uint(header[j+2u])<<8u)|uint(header[j+3u]); }\n"
"void compress(thread uint h[8], thread uint w[64]) {\n"
"  for (uint i=16u;i<64u;i++) { w[i]=ssig1(w[i-2u])+w[i-7u]+ssig0(w[i-15u])+w[i-16u]; }\n"
"  uint a=h[0],b=h[1],c=h[2],d=h[3],e=h[4],f=h[5],g=h[6],hh=h[7];\n"
"  for (uint i=0u;i<64u;i++) { uint t1=hh+bsig1(e)+ch(e,f,g)+K[i]+w[i]; uint t2=bsig0(a)+maj(a,b,c); hh=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2; }\n"
"  h[0]+=a; h[1]+=b; h[2]+=c; h[3]+=d; h[4]+=e; h[5]+=f; h[6]+=g; h[7]+=hh;\n"
"}\n"
"void init_hash(thread uint h[8]) { h[0]=0x6a09e667u; h[1]=0xbb67ae85u; h[2]=0x3c6ef372u; h[3]=0xa54ff53au; h[4]=0x510e527fu; h[5]=0x9b05688cu; h[6]=0x1f83d9abu; h[7]=0x5be0cd19u; }\n"
"bool word_trailing_zero(uint word, uint n) { if (n==0u) return true; if (n>=32u) return word==0u; return (word & ((1u << n)-1u)) == 0u; }\n"
"bool has_trailing_zero_bits(thread uint h[8], uint difficulty) { uint r=difficulty; if (r<=32u) return word_trailing_zero(h[7],r); if (h[7]!=0u) return false; r-=32u; if (r<=32u) return word_trailing_zero(h[6],r); if (h[6]!=0u) return false; r-=32u; return word_trailing_zero(h[5],r); }\n"
"bool check_nonce(const device uchar *header, uint nonce, uint difficulty) {\n"
"  uint h[8]; uint w[64]; init_hash(h);\n"
"  for (uint i=0u;i<16u;i++) { w[i]=word_at(header,i); }\n"
"  compress(h,w);\n"
"  w[0]=word_at(header,16u); w[1]=word_at(header,17u); w[2]=word_at(header,18u); w[3]=((nonce&0xffu)<<24u)|(((nonce>>8u)&0xffu)<<16u)|(((nonce>>16u)&0xffu)<<8u)|((nonce>>24u)&0xffu);\n"
"  w[4]=0x80000000u; for(uint i=5u;i<15u;i++){w[i]=0u;} w[15]=640u; compress(h,w);\n"
"  uint h2[8]; init_hash(h2); for(uint i=0u;i<8u;i++){w[i]=h[i];} w[8]=0x80000000u; for(uint i=9u;i<15u;i++){w[i]=0u;} w[15]=256u; compress(h2,w);\n"
"  return has_trailing_zero_bits(h2,difficulty);\n"
"}\n"
"kernel void powcoin_grind(const device uchar *header [[buffer(0)]], constant Params &params [[buffer(1)]], device atomic_uint *result [[buffer(2)]], uint gid [[thread_position_in_grid]]) {\n"
"  if (gid>=params.count) return; if (atomic_load_explicit(result,memory_order_relaxed)!=0xffffffffu) return; uint nonce=params.start_nonce+gid;\n"
"  if (check_nonce(header,nonce,params.difficulty)) { uint expected=0xffffffffu; atomic_compare_exchange_weak_explicit(result,&expected,nonce,memory_order_relaxed,memory_order_relaxed); }\n"
"}\n";

struct Params { uint32_t start_nonce; uint32_t count; uint32_t difficulty; uint32_t pad; };

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
	if (@available(macOS 26.0, *)) { opts.languageVersion = MTLLanguageVersion4_0; }
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
		id<MTLBuffer> headerBuffer = [g_device newBufferWithBytes:header length:80 options:MTLResourceStorageModeShared];
		id<MTLBuffer> paramsBuffer = [g_device newBufferWithLength:sizeof(struct Params) options:MTLResourceStorageModeShared];
		id<MTLBuffer> resultBuffer = [g_device newBufferWithLength:sizeof(uint32_t) options:MTLResourceStorageModeShared];
		if (headerBuffer == nil || paramsBuffer == nil || resultBuffer == nil) { set_error(errbuf, errbuf_len, @"allocate Metal buffers"); return 1; }
		uint32_t current = start_nonce; uint64_t remaining = limit;
		while (remaining > 0) {
			uint32_t count = remaining > batch_size ? batch_size : (uint32_t)remaining;
			*(uint32_t *)resultBuffer.contents = UINT32_MAX;
			struct Params *params = (struct Params *)paramsBuffer.contents;
			params->start_nonce = current; params->count = count; params->difficulty = difficulty; params->pad = 0;
			id<MTLCommandBuffer> cb = [g_queue commandBuffer];
			id<MTLComputeCommandEncoder> enc = [cb computeCommandEncoder];
			[enc setComputePipelineState:g_pipeline]; [enc setBuffer:headerBuffer offset:0 atIndex:0]; [enc setBuffer:paramsBuffer offset:0 atIndex:1]; [enc setBuffer:resultBuffer offset:0 atIndex:2];
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
