//go:build !js && !cuda

package shasolve

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/allbackends"
)

type WebGPUSolver struct {
	BatchSize uint32
	device    string
}

func (WebGPUSolver) Name() string { return "webgpu" }

func (s *WebGPUSolver) Solve(ctx context.Context, job Job) (Result, error) {
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

	instance, err := wgpu.CreateInstance(nil)
	if err != nil {
		return Result{}, err
	}
	defer instance.Release()

	adapter, err := instance.RequestAdapter(nil)
	if err != nil {
		return Result{}, err
	}
	defer adapter.Release()
	s.device = adapter.Info().Name

	device, err := adapter.RequestDevice(nil)
	if err != nil {
		return Result{}, err
	}
	defer device.Release()

	shaderSource := buildWebGPUShader(job.Header, job.Difficulty)
	shader, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "powcoin-shader",
		WGSL:  shaderSource,
	})
	if err != nil {
		return Result{}, err
	}
	defer shader.Release()

	paramsBuffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "powcoin-params-buffer",
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		Size:  16,
	})
	if err != nil {
		return Result{}, err
	}
	defer paramsBuffer.Release()

	resultBuffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "powcoin-result-buffer",
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc | wgpu.BufferUsageCopyDst,
		Size:  4,
	})
	if err != nil {
		return Result{}, err
	}
	defer resultBuffer.Release()

	readbackBuffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "powcoin-readback-buffer",
		Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
		Size:  4,
	})
	if err != nil {
		return Result{}, err
	}
	defer readbackBuffer.Release()

	layout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "powcoin-bind-group-layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{
					Type:           gputypes.BufferBindingTypeUniform,
					MinBindingSize: 16,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{
					Type:           gputypes.BufferBindingTypeStorage,
					MinBindingSize: 4,
				},
			},
		},
	})
	if err != nil {
		return Result{}, err
	}
	defer layout.Release()

	bindGroup, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "powcoin-bind-group",
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: paramsBuffer, Size: 16},
			{Binding: 1, Buffer: resultBuffer, Size: 4},
		},
	})
	if err != nil {
		return Result{}, err
	}
	defer bindGroup.Release()

	pipelineLayout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "powcoin-pipeline-layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{layout},
	})
	if err != nil {
		return Result{}, err
	}
	defer pipelineLayout.Release()

	pipeline, err := device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:      "powcoin-pipeline",
		Layout:     pipelineLayout,
		Module:     shader,
		EntryPoint: "main",
	})
	if err != nil {
		return Result{}, err
	}
	defer pipeline.Release()

	started := time.Now()
	current := job.StartNonce
	var tries uint64
	remaining := limit
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		count := job.BatchSize
		if uint64(count) > remaining {
			count = uint32(remaining)
		}
		if count == 0 {
			break
		}
		if err := device.Queue().WriteBuffer(resultBuffer, 0, u32Bytes(NotFound)); err != nil {
			return Result{}, err
		}
		if err := device.Queue().WriteBuffer(paramsBuffer, 0, paramsBytes(current, count)); err != nil {
			return Result{}, err
		}

		encoder, err := device.CreateCommandEncoder(nil)
		if err != nil {
			return Result{}, err
		}
		pass, err := encoder.BeginComputePass(nil)
		if err != nil {
			return Result{}, err
		}
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, bindGroup, nil)
		pass.Dispatch((count+255)/256, 1, 1)
		if err := pass.End(); err != nil {
			return Result{}, err
		}
		encoder.CopyBufferToBuffer(resultBuffer, 0, readbackBuffer, 0, 4)
		cmdBuffer, err := encoder.Finish()
		if err != nil {
			return Result{}, err
		}
		if _, err := device.Queue().Submit(cmdBuffer); err != nil {
			return Result{}, err
		}
		if err := readbackBuffer.Map(ctx, wgpu.MapModeRead, 0, 4); err != nil {
			return Result{}, err
		}
		rng, err := readbackBuffer.MappedRange(0, 4)
		if err != nil {
			_ = readbackBuffer.Unmap()
			return Result{}, err
		}
		found := binary.LittleEndian.Uint32(rng.Bytes())
		if err := readbackBuffer.Unmap(); err != nil {
			return Result{}, err
		}

		tries += uint64(count)
		if found != NotFound {
			return result("webgpu", s.device, job, found, tries, started), nil
		}
		if NotFound-current < count {
			break
		}
		current += count
		remaining -= uint64(count)
	}
	return Result{}, fmt.Errorf("no solution in %d hashes", tries)
}

func u32Bytes(v uint32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, v)
	return buf
}

func paramsBytes(startNonce, count uint32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], startNonce)
	binary.LittleEndian.PutUint32(buf[4:8], count)
	return buf
}

func powcoinMidstate(header [80]byte) ([8]uint32, [3]uint32) {
	h := [8]uint32{
		0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
		0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
	}
	var w [16]uint32
	for i := range w {
		w[i] = headerWord(header, uint32(i))
	}
	cpuSHA256Compress(&h, &w)
	return h, [3]uint32{
		headerWord(header, 16),
		headerWord(header, 17),
		headerWord(header, 18),
	}
}

func headerWord(header [80]byte, idx uint32) uint32 {
	j := idx * 4
	return uint32(header[j])<<24 | uint32(header[j+1])<<16 | uint32(header[j+2])<<8 | uint32(header[j+3])
}

func cpuSHA256Compress(h *[8]uint32, w *[16]uint32) {
	a, b, c, d := h[0], h[1], h[2], h[3]
	e, f, g, hh := h[4], h[5], h[6], h[7]
	for i, k := range sha256RoundConstants {
		wi := w[i&15]
		if i >= 16 {
			wi = ssig1CPU(w[(i-2)&15]) + w[(i-7)&15] + ssig0CPU(w[(i-15)&15]) + w[i&15]
			w[i&15] = wi
		}
		t1 := hh + bsig1CPU(e) + chCPU(e, f, g) + k + wi
		t2 := bsig0CPU(a) + majCPU(a, b, c)
		hh, g, f, e, d, c, b, a = g, f, e, d+t1, c, b, a, t1+t2
	}
	h[0] += a
	h[1] += b
	h[2] += c
	h[3] += d
	h[4] += e
	h[5] += f
	h[6] += g
	h[7] += hh
}

func rotrCPU(x uint32, n uint) uint32 { return (x >> n) | (x << (32 - n)) }
func chCPU(x, y, z uint32) uint32     { return (x & y) ^ (^x & z) }
func majCPU(x, y, z uint32) uint32    { return (x & y) ^ (x & z) ^ (y & z) }
func bsig0CPU(x uint32) uint32        { return rotrCPU(x, 2) ^ rotrCPU(x, 13) ^ rotrCPU(x, 22) }
func bsig1CPU(x uint32) uint32        { return rotrCPU(x, 6) ^ rotrCPU(x, 11) ^ rotrCPU(x, 25) }
func ssig0CPU(x uint32) uint32        { return rotrCPU(x, 7) ^ rotrCPU(x, 18) ^ (x >> 3) }
func ssig1CPU(x uint32) uint32        { return rotrCPU(x, 17) ^ rotrCPU(x, 19) ^ (x >> 10) }

var sha256RoundConstants = [...]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
	0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
	0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
	0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
	0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
	0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
	0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
	0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
	0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
	0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
	0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
	0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
	0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
	0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

func buildWebGPUShader(header [80]byte, difficulty uint32) string {
	mid, tail := powcoinMidstate(header)
	shader := strings.Replace(webgpuShaderTemplate, "{{CONSTANTS}}", buildWebGPUConstants(mid, tail), 1)
	shader = strings.Replace(shader, "{{COMPRESS}}", buildWebGPUCompress(), 1)
	return strings.Replace(shader, "{{DIFFICULTY_CHECK}}", buildWebGPUDifficultyCheck(difficulty), 1)
}

func buildWebGPUConstants(mid [8]uint32, tail [3]uint32) string {
	var b strings.Builder
	for i, v := range mid {
		fmt.Fprintf(&b, "const MID_%d: u32 = 0x%08xu;\n", i, v)
	}
	for i, v := range tail {
		fmt.Fprintf(&b, "const TAIL_%d: u32 = 0x%08xu;\n", i, v)
	}
	return b.String()
}

func buildWebGPUDifficultyCheck(difficulty uint32) string {
	if difficulty == 0 {
		return "    return true;\n"
	}
	var conditions []string
	remaining := difficulty
	for _, word := range []string{"h7", "h6", "h5"} {
		if remaining == 0 {
			break
		}
		if remaining >= 32 {
			conditions = append(conditions, fmt.Sprintf("%s == 0u", word))
			remaining -= 32
			continue
		}
		mask := (uint32(1) << remaining) - 1
		conditions = append(conditions, fmt.Sprintf("(%s & 0x%08xu) == 0u", word, mask))
		break
	}
	return fmt.Sprintf("    return %s;\n", strings.Join(conditions, " && "))
}

func buildWebGPUCompress() string {
	var b strings.Builder
	b.WriteString("fn compress(h: ptr<function, array<u32, 8>>, w: ptr<function, array<u32, 16>>) {\n")
	b.WriteString("    var a = (*h)[0]; var b = (*h)[1]; var c = (*h)[2]; var d = (*h)[3];\n")
	b.WriteString("    var e = (*h)[4]; var f = (*h)[5]; var g = (*h)[6]; var hh = (*h)[7];\n")
	for i, k := range sha256RoundConstants {
		if i >= 16 {
			fmt.Fprintf(&b,
				"    (*w)[%du] = ssig1((*w)[%du]) + (*w)[%du] + ssig0((*w)[%du]) + (*w)[%du];\n",
				i&15, (i-2)&15, (i-7)&15, (i-15)&15, i&15)
		}
		fmt.Fprintf(&b, "    let t1_%d = hh + bsig1(e) + ch(e, f, g) + 0x%08xu + (*w)[%du];\n", i, k, i&15)
		fmt.Fprintf(&b, "    let t2_%d = bsig0(a) + maj(a, b, c);\n", i)
		fmt.Fprintf(&b, "    hh = g; g = f; f = e; e = d + t1_%d; d = c; c = b; b = a; a = t1_%d + t2_%d;\n", i, i, i)
	}
	b.WriteString("    (*h)[0] = (*h)[0] + a; (*h)[1] = (*h)[1] + b; (*h)[2] = (*h)[2] + c; (*h)[3] = (*h)[3] + d;\n")
	b.WriteString("    (*h)[4] = (*h)[4] + e; (*h)[5] = (*h)[5] + f; (*h)[6] = (*h)[6] + g; (*h)[7] = (*h)[7] + hh;\n")
	b.WriteString("}\n")
	return b.String()
}

const webgpuShaderTemplate = `
struct Params {
    start_nonce: u32,
    count: u32,
    pad0: u32,
    pad1: u32,
}

struct Result {
    nonce: atomic<u32>,
}

{{CONSTANTS}}

@group(0) @binding(0) var<uniform> params: Params;
@group(0) @binding(1) var<storage, read_write> result: Result;

fn rotr(x: u32, n: u32) -> u32 { return (x >> n) | (x << (32u - n)); }
fn ch(x: u32, y: u32, z: u32) -> u32 { return (x & y) ^ ((~x) & z); }
fn maj(x: u32, y: u32, z: u32) -> u32 { return (x & y) ^ (x & z) ^ (y & z); }
fn bsig0(x: u32) -> u32 { return rotr(x, 2u) ^ rotr(x, 13u) ^ rotr(x, 22u); }
fn bsig1(x: u32) -> u32 { return rotr(x, 6u) ^ rotr(x, 11u) ^ rotr(x, 25u); }
fn ssig0(x: u32) -> u32 { return rotr(x, 7u) ^ rotr(x, 18u) ^ (x >> 3u); }
fn ssig1(x: u32) -> u32 { return rotr(x, 17u) ^ rotr(x, 19u) ^ (x >> 10u); }

fn nonce_word(nonce: u32) -> u32 {
    return ((nonce & 0xffu) << 24u) | (((nonce >> 8u) & 0xffu) << 16u) | (((nonce >> 16u) & 0xffu) << 8u) | ((nonce >> 24u) & 0xffu);
}

fn init_hash(h: ptr<function, array<u32, 8>>) {
    (*h)[0] = 0x6a09e667u; (*h)[1] = 0xbb67ae85u; (*h)[2] = 0x3c6ef372u; (*h)[3] = 0xa54ff53au;
    (*h)[4] = 0x510e527fu; (*h)[5] = 0x9b05688cu; (*h)[6] = 0x1f83d9abu; (*h)[7] = 0x5be0cd19u;
}

{{COMPRESS}}

fn has_trailing_difficulty(h5: u32, h6: u32, h7: u32) -> bool {
{{DIFFICULTY_CHECK}}
}

fn check_nonce(nonce: u32) -> bool {
    var h: array<u32, 8>;
    var h2: array<u32, 8>;
    var w: array<u32, 16>;
    h[0] = MID_0; h[1] = MID_1; h[2] = MID_2; h[3] = MID_3;
    h[4] = MID_4; h[5] = MID_5; h[6] = MID_6; h[7] = MID_7;

    w[0] = TAIL_0;
    w[1] = TAIL_1;
    w[2] = TAIL_2;
    w[3] = nonce_word(nonce);
    w[4] = 0x80000000u;
    for (var i = 5u; i < 15u; i = i + 1u) { w[i] = 0u; }
    w[15] = 640u;
    compress(&h, &w);

    init_hash(&h2);
    for (var i = 0u; i < 8u; i = i + 1u) { w[i] = h[i]; }
    w[8] = 0x80000000u;
    for (var i = 9u; i < 15u; i = i + 1u) { w[i] = 0u; }
    w[15] = 256u;
    compress(&h2, &w);

    return has_trailing_difficulty(h2[5], h2[6], h2[7]);
}

@compute @workgroup_size(256)
fn main(@builtin(global_invocation_id) id: vec3<u32>) {
    let gid = id.x;
    if (gid >= params.count) { return; }
    let nonce = params.start_nonce + gid;
    if (check_nonce(nonce)) {
        var expected = 0xffffffffu;
        _ = atomicCompareExchangeWeak(&result.nonce, expected, nonce);
    }
}
`
