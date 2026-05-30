(() => {
  "use strict";

  const encoder = new TextEncoder();
  const batchSize = 1 << 20;

  const wasmReady = initWasm();
  const shaderReady = fetch(chrome.runtime.getURL("shader_unrolled.wgsl")).then((r) => {
    if (!r.ok) throw new Error(`could not load shader: HTTP ${r.status}`);
    return r.text();
  });

  function ready(fn) {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", fn, { once: true });
    } else {
      fn();
    }
  }

  ready(() => {
    const form = document.getElementById("faucet-form");
    if (!form) return;
    installTestButton(form);

    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      event.stopImmediatePropagation();
      await runWebGpuFlow();
    }, true);
  });

  function installTestButton(form) {
    if (document.getElementById("webgpu-test")) return;

    const submitEl = document.getElementById("submit");
    const button = document.createElement("button");
    button.id = "webgpu-test";
    button.type = "button";
    button.textContent = "Test WebGPU PoW";
    button.style.marginTop = "0.25rem";
    button.addEventListener("click", async () => {
      await runWebGpuTest(button);
    });

    if (submitEl?.parentNode === form) {
      submitEl.insertAdjacentElement("afterend", button);
    } else {
      form.appendChild(button);
    }
  }

  async function initWasm() {
    if (typeof Go !== "function") {
      throw new Error("Go WASM runtime was not loaded");
    }
    const go = new Go();
    const wasmURL = chrome.runtime.getURL("faucetpow.wasm");
    const response = await fetch(wasmURL);
    if (!response.ok) {
      throw new Error(`could not load WASM: HTTP ${response.status}`);
    }
    const bytes = await response.arrayBuffer();
    const result = await WebAssembly.instantiate(bytes, go.importObject);
    go.run(result.instance);
  }

  async function runWebGpuFlow() {
    const addressEl = document.getElementById("address");
    const submitEl = document.getElementById("submit");
    const address = addressEl?.value?.trim();
    if (!address) return;

    if (!navigator.gpu) {
      showStatus("err", "WebGPU is not available in this browser.");
      return;
    }

    submitEl.disabled = true;

    try {
      await wasmReady;

      showStatus("working", "Requesting WebGPU challenge&hellip;");
      const challengeResponse = await fetch(new URL("/api/challenge", location.href));
      const challengeJSON = await challengeResponse.json();
      if (!challengeResponse.ok) {
        throw new Error(challengeJSON.error || `HTTP ${challengeResponse.status}`);
      }

      showStatus("working",
        `Solving SHA-256 proof of work with WebGPU (${challengeJSON.difficulty} bits)&hellip;` +
        "<div class='progress' id='webgpu-prog'>0 tries</div>");

      const progressEl = document.getElementById("webgpu-prog");
      const solved = await solvePoWWebGPU(challengeJSON.challenge, challengeJSON.difficulty, (p) => {
        if (!progressEl) return;
        progressEl.textContent = `${p.tries.toLocaleString()} tries · ${p.seconds.toFixed(2)}s · ${p.mhps.toFixed(2)} MH/s · ${p.device}`;
      });

      const verification = faucetPowVerify(challengeJSON.challenge, solved.nonce, challengeJSON.difficulty);
      if (!verification.ok) {
        throw new Error(`WASM verification failed for nonce ${solved.nonce}`);
      }

      showStatus("working",
        `Solved with ${escapeHTML(solved.device)} in ${solved.tries.toLocaleString()} tries ` +
        `(${solved.seconds.toFixed(2)}s, ${solved.mhps.toFixed(2)} MH/s). Submitting&hellip;`);

      const faucetResponse = await fetch(new URL("/api/faucet", location.href), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          address,
          challenge: challengeJSON.challenge,
          nonce: solved.nonce,
        }),
      });
      const faucetJSON = await faucetResponse.json();
      if (!faucetResponse.ok) {
        throw new Error(faucetJSON.error || `HTTP ${faucetResponse.status}`);
      }

      showStatus("ok", `Sent ${faucetJSON.amount_btc} BTC.<br>txid: <code>${escapeHTML(faucetJSON.txid)}</code>`);
    } catch (error) {
      showStatus("err", escapeHTML(error.message || String(error)));
    } finally {
      submitEl.disabled = false;
    }
  }

  async function runWebGpuTest(button) {
    if (!navigator.gpu) {
      showStatus("err", "WebGPU is not available in this browser.");
      return;
    }

    const submitEl = document.getElementById("submit");
    button.disabled = true;
    if (submitEl) submitEl.disabled = true;

    try {
      await wasmReady;

      showStatus("working", "Requesting WebGPU test challenge&hellip;");
      const challengeResponse = await fetch(new URL("/api/challenge", location.href));
      const challengeJSON = await challengeResponse.json();
      if (!challengeResponse.ok) {
        throw new Error(challengeJSON.error || `HTTP ${challengeResponse.status}`);
      }

      showStatus("working",
        `Testing WebGPU proof of work (${challengeJSON.difficulty} bits)&hellip;` +
        "<div class='progress' id='webgpu-prog'>0 tries</div>");

      const progressEl = document.getElementById("webgpu-prog");
      const solved = await solvePoWWebGPU(challengeJSON.challenge, challengeJSON.difficulty, (p) => {
        if (!progressEl) return;
        progressEl.textContent = `${p.tries.toLocaleString()} tries · ${p.seconds.toFixed(2)}s · ${p.mhps.toFixed(2)} MH/s · ${p.device}`;
      });

      const verification = faucetPowVerify(challengeJSON.challenge, solved.nonce, challengeJSON.difficulty);
      if (!verification.ok) {
        throw new Error(`WASM verification failed for nonce ${solved.nonce}`);
      }

      showStatus("ok",
        `WebGPU test solved without submitting.<br>` +
        `Device: <code>${escapeHTML(solved.device)}</code><br>` +
        `Rate: <code>${solved.mhps.toFixed(2)} MH/s</code><br>` +
        `Nonce: <code>${escapeHTML(solved.nonce)}</code> · ` +
        `${solved.tries.toLocaleString()} tries · ${solved.seconds.toFixed(2)}s`);
    } catch (error) {
      showStatus("err", escapeHTML(error.message || String(error)));
    } finally {
      button.disabled = false;
      if (submitEl) submitEl.disabled = false;
    }
  }

  async function solvePoWWebGPU(challenge, difficulty, onProgress) {
    if (challenge.length !== 64) {
      throw new Error(`expected 64-byte ASCII challenge, got ${challenge.length}`);
    }

    const shader = await shaderReady;
    const adapter = await navigator.gpu.requestAdapter({ powerPreference: "high-performance" });
    if (!adapter) throw new Error("no WebGPU adapter available");

    const device = await adapter.requestDevice();
    const deviceName = adapter.info?.device || adapter.info?.description || adapter.info?.vendor || "WebGPU";

    const prefix = `${challenge}:`;
    const prefixWords = new Uint32Array(96);
    const prefixBytes = encoder.encode(prefix);
    for (let i = 0; i < prefixBytes.length; i++) {
      prefixWords[i] = prefixBytes[i];
    }

    const prefixBuffer = device.createBuffer({
      label: "prefix-buffer",
      size: prefixWords.byteLength,
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
    });
    const paramsBuffer = device.createBuffer({
      label: "params-buffer",
      size: 16,
      usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
    });
    const resultBuffer = device.createBuffer({
      label: "result-buffer",
      size: 4,
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC | GPUBufferUsage.COPY_DST,
    });
    const readbackBuffer = device.createBuffer({
      label: "readback-buffer",
      size: 4,
      usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST,
    });

    device.queue.writeBuffer(prefixBuffer, 0, prefixWords);

    const module = device.createShaderModule({ label: "pow-shader", code: shader });
    const bindGroupLayout = device.createBindGroupLayout({
      label: "pow-bind-group-layout",
      entries: [
        {
          binding: 0,
          visibility: GPUShaderStage.COMPUTE,
          buffer: { type: "read-only-storage", minBindingSize: 96 * 4 },
        },
        {
          binding: 1,
          visibility: GPUShaderStage.COMPUTE,
          buffer: { type: "uniform", minBindingSize: 16 },
        },
        {
          binding: 2,
          visibility: GPUShaderStage.COMPUTE,
          buffer: { type: "storage", minBindingSize: 4 },
        },
      ],
    });
    const pipeline = device.createComputePipeline({
      label: "pow-pipeline",
      layout: device.createPipelineLayout({ bindGroupLayouts: [bindGroupLayout] }),
      compute: { module, entryPoint: "main" },
    });
    const bindGroup = device.createBindGroup({
      label: "pow-bind-group",
      layout: bindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: prefixBuffer } },
        { binding: 1, resource: { buffer: paramsBuffer } },
        { binding: 2, resource: { buffer: resultBuffer } },
      ],
    });

    const params = new Uint32Array(4);
    const notFound = new Uint32Array([0xffffffff]);
    let startNonce = 0;
    const t0 = performance.now();

    while (true) {
      params[0] = prefixBytes.length;
      params[1] = startNonce;
      params[2] = batchSize;
      params[3] = difficulty;

      device.queue.writeBuffer(resultBuffer, 0, notFound);
      device.queue.writeBuffer(paramsBuffer, 0, params);

      const commandEncoder = device.createCommandEncoder();
      const pass = commandEncoder.beginComputePass();
      pass.setPipeline(pipeline);
      pass.setBindGroup(0, bindGroup);
      pass.dispatchWorkgroups(Math.ceil(batchSize / 256));
      pass.end();
      commandEncoder.copyBufferToBuffer(resultBuffer, 0, readbackBuffer, 0, 4);
      device.queue.submit([commandEncoder.finish()]);

      await readbackBuffer.mapAsync(GPUMapMode.READ);
      const found = new DataView(readbackBuffer.getMappedRange()).getUint32(0, true);
      readbackBuffer.unmap();

      if (found !== 0xffffffff) {
        const seconds = (performance.now() - t0) / 1000;
        const tries = found + 1;
        return {
          nonce: String(found),
          tries,
          seconds,
          mhps: tries / Math.max(seconds, 0.001) / 1_000_000,
          device: deviceName,
        };
      }

      startNonce += batchSize;
      const seconds = (performance.now() - t0) / 1000;
      onProgress({
        tries: startNonce,
        seconds,
        mhps: startNonce / Math.max(seconds, 0.001) / 1_000_000,
        device: deviceName,
      });

      await new Promise((resolve) => setTimeout(resolve, 0));
    }
  }

  function showStatus(kind, html) {
    let statusEl = document.getElementById("status");
    if (!statusEl) {
      statusEl = document.createElement("div");
      statusEl.id = "status";
      document.body.appendChild(statusEl);
    }
    statusEl.className = `status ${kind}`;
    statusEl.innerHTML = html;
    statusEl.style.display = "block";
  }

  function escapeHTML(value) {
    return String(value)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#039;");
  }
})();
