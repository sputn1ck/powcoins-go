//go:build !js

package faucetpow

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
	_ "github.com/gogpu/wgpu/hal/allbackends"
)

//go:embed src/shader_unrolled.wgsl
var shaderWGSL string

type GPUSolver struct {
	BatchSize uint32
	Progress  func(searched uint32)
	device    string
}

func (s *GPUSolver) Solve(ctx context.Context, challenge string, difficulty uint32) (Solution, error) {
	started := time.Now()
	nonce, err := s.solveNonce(ctx, challenge, difficulty)
	if err != nil {
		return Solution{}, err
	}
	sol := solution(challenge, nonce, started)
	sol.Device = s.device
	return sol, nil
}

func (s *GPUSolver) solveNonce(ctx context.Context, challenge string, difficulty uint32) (uint32, error) {
	batchSize := s.BatchSize
	if batchSize == 0 {
		batchSize = 1 << 20
	}

	prefix := challenge + ":"
	if len(challenge) != 64 {
		return 0, fmt.Errorf("GPU shader expects a 64-byte ASCII challenge, got %d bytes", len(challenge))
	}

	instance, err := wgpu.CreateInstance(nil)
	if err != nil {
		return 0, err
	}
	defer instance.Release()
	debugGPU("instance created")

	adapter, err := instance.RequestAdapter(nil)
	if err != nil {
		return 0, err
	}
	defer adapter.Release()
	adapterInfo := adapter.Info()
	s.device = adapterInfo.Name
	debugGPU("adapter requested: " + adapterInfo.Name)

	device, err := adapter.RequestDevice(nil)
	if err != nil {
		return 0, err
	}
	defer device.Release()
	debugGPU("device requested")

	shader, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "pow-shader",
		WGSL:  shaderWGSL,
	})
	if err != nil {
		return 0, err
	}
	defer shader.Release()
	debugGPU("shader created")

	prefixBuffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "prefix-buffer",
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst,
		Size:  96 * 4,
	})
	if err != nil {
		return 0, err
	}
	defer prefixBuffer.Release()
	prefixData := make([]byte, 96*4)
	for i := 0; i < len(prefix); i++ {
		binary.LittleEndian.PutUint32(prefixData[i*4:], uint32(prefix[i]))
	}
	if err := device.Queue().WriteBuffer(prefixBuffer, 0, prefixData); err != nil {
		return 0, err
	}

	paramsBuffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "params-buffer",
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
		Size:  16,
	})
	if err != nil {
		return 0, err
	}
	defer paramsBuffer.Release()

	resultBuffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "result-buffer",
		Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc | wgpu.BufferUsageCopyDst,
		Size:  4,
	})
	if err != nil {
		return 0, err
	}
	defer resultBuffer.Release()

	readbackBuffer, err := device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "readback-buffer",
		Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
		Size:  4,
	})
	if err != nil {
		return 0, err
	}
	defer readbackBuffer.Release()

	layout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "pow-bind-group-layout",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{
					Type:           gputypes.BufferBindingTypeReadOnlyStorage,
					MinBindingSize: 96 * 4,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{
					Type:           gputypes.BufferBindingTypeUniform,
					MinBindingSize: 16,
				},
			},
			{
				Binding:    2,
				Visibility: wgpu.ShaderStageCompute,
				Buffer: &gputypes.BufferBindingLayout{
					Type:           gputypes.BufferBindingTypeStorage,
					MinBindingSize: 4,
				},
			},
		},
	})
	if err != nil {
		return 0, err
	}
	defer layout.Release()

	bindGroup, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "pow-bind-group",
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: prefixBuffer, Size: 96 * 4},
			{Binding: 1, Buffer: paramsBuffer, Size: 16},
			{Binding: 2, Buffer: resultBuffer, Size: 4},
		},
	})
	if err != nil {
		return 0, err
	}
	defer bindGroup.Release()

	pipelineLayout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label:            "pow-pipeline-layout",
		BindGroupLayouts: []*wgpu.BindGroupLayout{layout},
	})
	if err != nil {
		return 0, err
	}
	defer pipelineLayout.Release()

	pipeline, err := device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Label:      "pow-pipeline",
		Layout:     pipelineLayout,
		Module:     shader,
		EntryPoint: "main",
	})
	if err != nil {
		return 0, err
	}
	defer pipeline.Release()

	var startNonce uint32
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		if err := device.Queue().WriteBuffer(resultBuffer, 0, u32Bytes(NotFound)); err != nil {
			return 0, err
		}
		if err := device.Queue().WriteBuffer(paramsBuffer, 0, paramsBytes(uint32(len(prefix)), startNonce, batchSize, difficulty)); err != nil {
			return 0, err
		}

		encoder, err := device.CreateCommandEncoder(nil)
		if err != nil {
			return 0, err
		}
		pass, err := encoder.BeginComputePass(nil)
		if err != nil {
			return 0, err
		}
		pass.SetPipeline(pipeline)
		pass.SetBindGroup(0, bindGroup, nil)
		pass.Dispatch((batchSize+255)/256, 1, 1)
		if err := pass.End(); err != nil {
			return 0, err
		}

		encoder.CopyBufferToBuffer(resultBuffer, 0, readbackBuffer, 0, 4)
		cmdBuffer, err := encoder.Finish()
		if err != nil {
			return 0, err
		}
		if _, err := device.Queue().Submit(cmdBuffer); err != nil {
			return 0, err
		}
		debugGPU("submitted batch")

		if err := readbackBuffer.Map(ctx, wgpu.MapModeRead, 0, 4); err != nil {
			return 0, err
		}
		rng, err := readbackBuffer.MappedRange(0, 4)
		if err != nil {
			_ = readbackBuffer.Unmap()
			return 0, err
		}
		found := binary.LittleEndian.Uint32(rng.Bytes())
		if err := readbackBuffer.Unmap(); err != nil {
			return 0, err
		}

		if found != NotFound {
			return found, nil
		}

		next := startNonce + batchSize
		if next < startNonce {
			return 0, fmt.Errorf("searched entire u32 nonce space")
		}
		startNonce = next
		if s.Progress != nil {
			s.Progress(startNonce)
		}
	}
}

func debugGPU(msg string) {
	if os.Getenv("FAUCETPOW_GPU_DEBUG") != "" {
		log.Printf("gpu: %s", msg)
	}
}

func u32Bytes(v uint32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, v)
	return buf
}
