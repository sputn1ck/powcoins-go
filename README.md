# powcoins-go

Go CLI and library for claiming coins from
[`ajtowns/powcoins`](https://github.com/ajtowns/powcoins) on Bitcoin Signet.

It scans Signet Esplora address history for faucet UTXOs, builds the Taproot
script-path claim transaction, grinds the fake block header proof, and relays
the transaction directly to the Bitcoin Inquisition Signet peer over P2P.
The grinder can also use the native Metal or WebGPU solver backends.

## Usage

Build the CLI:

```sh
go build -o powcoins-go ./cmd/powcoins-go
```

Or install it directly from the renamed GitHub repo:

```sh
go install github.com/sputn1ck/powcoins-go/cmd/powcoins-go@latest
```

Dry-run without relaying:

```sh
./powcoins-go -address <signet-address> -dry-run
```

Claim and relay:

```sh
./powcoins-go -address <signet-address>
```

After scanning, the CLI prompts before grinding each candidate UTXO:

```text
[y]es, [e]stimate, [s]kip, [c]ancel
```

`yes` mines and spends that UTXO, `skip` moves to the next candidate, and
`cancel` exits. `estimate` runs a short difficulty-21 benchmark on the selected
solver backend and prints the measured MH/s plus the expected solve time for
the displayed UTXO difficulty.

Useful options:

```sh
./powcoins-go \
  -address <signet-address> \
  -solver auto \
  -min-difficulty 20 \
  -max-difficulty 26 \
  -max-pages 40 \
  -fee-rate 1 \
  -peer-messages \
  -peer-listen 5s \
  -relay-peer inquisition.bitcoin-signet.net:38333
```

The default Esplora endpoint is `https://mempool.space/signet/api`. Public
Esplora instances can cap large faucet UTXO sets, so the CLI scans paged address
history and derives unspent outputs locally instead of relying on
`/address/:address/utxo`.

`-peer-messages` prints P2P commands sent and received during relay. With
`-peer-listen`, the CLI keeps the peer connection open after sending the
transaction so you can inspect follow-up messages such as `reject`, `notfound`,
or `ping`.

`-min-difficulty` and `-max-difficulty` define the UTXO selection range. Raising
the minimum can avoid the most contested low-difficulty floor, at the cost of
more grinding. `-fee-rate` and `-feerate` are aliases for the manual fee-rate
override in sat/vB; the default is `1`.

The faucet is raceable: every faucet UTXO can be claimed by anyone, and the
first valid spend that propagates and confirms wins. The lowest-difficulty
coins are fastest to grind, but they are also the easiest targets for everyone
else. Setting `-min-difficulty` above the floor, for example `20` or `24`, can
improve your chances by selecting less-contested UTXOs, though each additional
difficulty bit roughly doubles the expected grind time.

`-solver` selects the proof-of-work backend. The default is `auto`, which tries
`metal` first, then `cuda`, then `webgpu`, then `cpu`. You can override it with
`cpu`, `metal`, `cuda`, or `webgpu`. The Metal backend requests Metal language 4.0 on macOS
versions that expose it.

The command builds on non-mac platforms. Metal code is compiled only on
`darwin` with cgo, and CUDA is compiled only for Linux cgo builds with
`-tags cuda`; other builds use stubs that report those backends as unavailable,
so `-solver auto` proceeds to the next backend.

At 5 MH/s, expected average solve times are approximately:

| Difficulty | Expected time |
| --- | ---: |
| 16 | 0.013 s |
| 26 | 13.4 s |
| 36 | 3.8 h |
| 46 | 162.9 days |
| 56 | 456.7 years |
| 66 | 467,633.6 years |
| 76 | 478,856,844.2 years |
| 80 | 7,661,709,506.5 years |

## Library

The claim builder can be used as a Go package:

```go
import "github.com/sputn1ck/powcoins-go/powcoins"

result, err := powcoins.BuildClaim(ctx, powcoins.ClaimOptions{
    Address:       "tb1...",
    MinDifficulty: 20,
    MaxDifficulty: 26,
    FeeRate:       1,
})
```

Then relay the mined transaction:

```go
err = powcoins.RelayTx(powcoins.DefaultRelayPeer, result.Tx, 0)
```

## Notes

This is Signet-only demo software. It uses the fixed key and Taproot scripts
from the original powcoins faucet construction, and it expects an
Inquisition-compatible relay peer for OP_CAT spends.

## SHA Solver Benchmarks

The `shasolve` package provides a small HAL for benchmarking SHA PoW solvers
with the common job format:

```text
double_sha256(80-byte powcoin fake block header with uint32_le nonce at bytes 76..79)
```

Available native backends:

- `metal`: direct Apple Metal compute backend.
- `cuda`: NVIDIA CUDA backend using the CUDA driver API and NVRTC.
- `webgpu`: native WebGPU backend through `github.com/gogpu/wgpu`.
- `cpu`: reference implementation for correctness checks.

The CUDA backend is opt-in because it adds native C objects to the build:

```sh
go run -tags cuda ./cmd/sha-bench -difficulty 28 -backends cuda
```

The default build keeps the WebGPU backend enabled. CUDA-tagged builds disable
the WebGPU backend because the current WebGPU dependency uses dynamic-import
assembly that does not link together with package-local C objects.

On an RTX 4090 under WSL2, the optimized CUDA backend has measured roughly
`6.7-7.2 GH/s` on the deterministic difficulty-28/29 benchmark job. It
precomputes the nonce-independent SHA-256 midstate on the CPU and uses a
16-word rolling SHA schedule in the CUDA kernel.

Run the difficulty-28 comparison command:

```sh
go run ./cmd/sha-bench -difficulty 28 -backends metal,webgpu
```

The benchmark uses the same powcoin witness-building rule as the claim path:
the fake block header is built from a deterministic 64-byte signature, the nonce
is ground in the header, and difficulty is measured as trailing zero bits in the
Bitcoin double-SHA256 result. GPU batches default to `16,776,960` candidates,
which is WebGPU's 65535 workgroups by 256 threads dispatch limit.

Or run the Go benchmarks with a single solve per backend:

```sh
go test ./shasolve -bench=Difficulty28 -benchtime=1x -run '^$'
```

Both GPU backends implement the same `shasolve.Solver` interface and verify the
returned nonce with the CPU double-SHA256 implementation.
