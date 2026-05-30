package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	faucetpow "github.com/kon/webgpu-sha-faucet"
)

func main() {
	var (
		baseURL    = flag.String("url", faucetpow.DefaultURL, "faucet base URL")
		address    = flag.String("address", "", "signet address to fund; when set, submits to /api/faucet")
		submit     = flag.Bool("submit", false, "submit solved PoW to /api/faucet; implied by -address")
		backend    = flag.String("backend", "gpu", "solver backend: gpu or cpu")
		challenge  = flag.String("challenge", "", "solve this challenge instead of fetching one")
		difficulty = flag.Uint("difficulty", 0, "difficulty for -challenge")
		batchSize  = flag.Uint("batch-size", 1<<20, "nonces tested per GPU dispatch")
	)
	flag.Parse()

	shouldSubmit := *address != "" || *submit
	if shouldSubmit && *address == "" {
		log.Fatal("-address is required to submit")
	}
	if (*challenge == "") != (*difficulty == 0) {
		log.Fatal("-challenge and -difficulty must be provided together")
	}
	if shouldSubmit && *challenge != "" {
		log.Fatal("-address/-submit cannot be used with -challenge because faucet challenges expire")
	}

	ctx := context.Background()
	client := faucetpow.NewClient(*baseURL)

	var chal faucetpow.Challenge
	if *challenge != "" {
		chal = faucetpow.Challenge{Challenge: *challenge, Difficulty: uint32(*difficulty)}
	} else {
		info, err := client.Info(ctx)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("faucet: network=%s payout=%g BTC balance=%g BTC pow=%d bits\n",
			info.Network, info.PayoutBTC, info.BalanceBTC, info.PoWDifficulty)

		chal, err = client.Challenge(ctx)
		if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("challenge: %s difficulty=%d expires=%ds\n", chal.Challenge, chal.Difficulty, chal.ExpiresInSeconds)

	var solver faucetpow.Solver
	switch *backend {
	case "gpu":
		solver = &faucetpow.GPUSolver{
			BatchSize: uint32(*batchSize),
			Progress: func(searched uint32) {
				fmt.Printf("searched %d nonces...\n", searched)
			},
		}
	case "cpu":
		solver = faucetpow.CPUSolver{}
	default:
		log.Fatalf("unknown backend %q", *backend)
	}

	started := time.Now()
	sol, err := solver.Solve(ctx, chal.Challenge, chal.Difficulty)
	if err != nil {
		log.Fatal(err)
	}
	sol.Elapsed = time.Since(started)
	sol.HashesPerSecond = float64(sol.Tries) / max(sol.Elapsed.Seconds(), 0.001)

	if zeros, ok := faucetpow.Verify(chal.Challenge, sol.NonceString, chal.Difficulty); !ok {
		log.Fatalf("internal verification failed: leading_zero_bits=%d difficulty=%d", zeros, chal.Difficulty)
	}

	device := sol.Device
	if device == "" {
		device = *backend
	}
	fmt.Printf("solved: nonce=%s tries=%d elapsed=%.3fs rate=%.2f MH/s device=%q leading_zero_bits=%d\n",
		sol.NonceString, sol.Tries, sol.Elapsed.Seconds(), sol.HashesPerSecond/1_000_000, device, sol.LeadingZeroBits)

	if shouldSubmit {
		resp, err := client.Submit(ctx, *address, chal.Challenge, sol.NonceString)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("submitted: sent=%g BTC txid=%s\n", resp.AmountBTC, resp.TxID)
	} else {
		fmt.Println("dry-run: not submitted; pass -address <signet-address> to request coins")
	}
}
