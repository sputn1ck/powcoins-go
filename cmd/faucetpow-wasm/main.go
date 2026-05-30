//go:build js && wasm

package main

import (
	"strconv"
	"syscall/js"

	faucetpow "github.com/kon/webgpu-sha-faucet"
)

func main() {
	js.Global().Set("faucetPowVerify", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 3 {
			return map[string]any{"ok": false, "error": "expected challenge, nonce, difficulty"}
		}

		challenge := args[0].String()
		nonce := args[1].String()
		difficulty := uint32(args[2].Int())

		zeros, ok := faucetpow.Verify(challenge, nonce, difficulty)
		return map[string]any{
			"ok":               ok,
			"leadingZeroBits":  zeros,
			"challenge":        challenge,
			"nonce":            nonce,
			"difficulty":       difficulty,
			"hashesAsExpected": ok,
		}
	}))

	js.Global().Set("faucetPowDigestBits", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 2 {
			return map[string]any{"error": "expected challenge and nonce"}
		}

		zeros := faucetpow.LeadingZeroBits(faucetpow.DigestPoW(args[0].String(), args[1].String()))
		return map[string]any{
			"leadingZeroBits": zeros,
		}
	}))

	js.Global().Set("faucetPowVersion", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		return map[string]any{
			"name":     "webgpu-sha-faucet",
			"wasm":     true,
			"maxNonce": strconv.FormatUint(uint64(faucetpow.NotFound), 10),
		}
	}))

	select {}
}
