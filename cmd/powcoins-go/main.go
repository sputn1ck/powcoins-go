package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/sputn1ck/powcoins-go/powcoins"
	"github.com/sputn1ck/powcoins-go/shasolve"
)

func main() {
	var feeRate int64
	var (
		address      = flag.String("address", "", "signet address to receive claimed coins")
		esplora      = flag.String("esplora", powcoins.DefaultEsploraURL, "signet Esplora API base URL")
		relayPeer    = flag.String("relay-peer", powcoins.DefaultRelayPeer, "signet P2P peer that accepts OP_CAT spends")
		maxPages     = flag.Int("max-pages", 40, "maximum Esplora history pages to scan per faucet address")
		minDiff      = flag.Int("min-difficulty", 0, "minimum PoW difficulty to select; useful to avoid highly contested low-difficulty UTXOs")
		maxDiff      = flag.Int("max-difficulty", 35, "maximum PoW difficulty to attempt")
		dryRun       = flag.Bool("dry-run", false, "build and mine the transaction without relaying it")
		relayTimeout = flag.Duration("relay-timeout", 30*time.Second, "P2P relay timeout")
		peerMessages = flag.Bool("peer-messages", false, "print P2P messages during relay")
		peerListen   = flag.Duration("peer-listen", 0, "after sending the tx, keep reading peer messages for this duration")
		solverName   = flag.String("solver", "auto", "PoW solver: auto, cpu, metal, cuda, or webgpu")
	)
	flag.Int64Var(&feeRate, "feerate", 0, "fee rate override in sat/vB; default is 1")
	flag.Int64Var(&feeRate, "fee-rate", 0, "alias for -feerate")
	flag.Parse()

	if *address == "" {
		log.Fatal("-address is required")
	}
	ctx := context.Background()
	backend, err := selectBackend(ctx, *solverName)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("solver: %s", backend.name)
	if backend.device != "" {
		fmt.Printf(" (%s)", backend.device)
	}
	fmt.Println()

	prompter := newCandidatePrompter(backend)
	result, err := powcoins.BuildClaim(ctx, powcoins.ClaimOptions{
		Address:       *address,
		EsploraURL:    *esplora,
		MaxPages:      *maxPages,
		MinDifficulty: *minDiff,
		MaxDifficulty: *maxDiff,
		FeeRate:       feeRate,
		GrindSolver:   backend.grind,
		ConfirmUTXO:   prompter.confirm,
		Progress: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
		GrindProgress: func(tries uint64, elapsedSeconds float64, mhps float64) {
			fmt.Printf("grind: tries=%d elapsed=%.1fs rate=%.2f MH/s\n", tries, elapsedSeconds, mhps)
		},
	})
	if err != nil {
		if errors.Is(err, powcoins.ErrClaimCanceled) {
			fmt.Println("canceled")
			return
		}
		log.Fatal(err)
	}

	fmt.Printf("mined: solver=%s nonce=%d tries=%d elapsed=%.3fs rate=%.2f MH/s\n",
		backend.name,
		result.Grind.Nonce, result.Grind.Tries, result.Grind.Elapsed.Seconds(),
		result.Grind.HashesPerSecond/1_000_000)
	fmt.Printf("tx: txid=%s output=%d sats fee=%d sats feerate=%d sat/vB\n",
		result.TxID, result.OutputValue, result.Fee, result.FeeRate)

	if *dryRun {
		fmt.Printf("dry-run: not relayed\nrawtx: %s\n", result.TxHex)
		return
	}

	fmt.Printf("relaying to %s...\n", *relayPeer)
	var onMessage func(powcoins.PeerMessage)
	if *peerMessages {
		onMessage = func(msg powcoins.PeerMessage) {
			fmt.Printf("peer %s %s%s\n", msg.Direction, msg.Command, peerMessageDetails(msg))
		}
	}
	if err := powcoins.RelayTxWithOptions(result.Tx, powcoins.RelayOptions{
		PeerAddr:      *relayPeer,
		Timeout:       *relayTimeout,
		ListenAfterTx: *peerListen,
		OnMessage:     onMessage,
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("relayed: txid=%s\n", result.TxID)
}

type backendChoice struct {
	name     string
	grind    shasolve.Solver
	estimate shasolve.Solver
	device   string
	hps      float64
}

func selectBackend(ctx context.Context, name string) (backendChoice, error) {
	switch strings.ToLower(name) {
	case "auto":
		return autoBackend(ctx)
	case "cpu":
		return backendChoice{name: "cpu", estimate: shasolve.CPUSolver{}, device: "cpu"}, nil
	case "metal":
		solver := shasolve.MetalSolver{}
		result, err := probeSolver(ctx, solver)
		if err != nil {
			return backendChoice{}, err
		}
		return backendChoice{name: "metal", grind: solver, estimate: solver, device: result.Device}, nil
	case "cuda":
		solver := shasolve.CUDASolver{}
		result, err := probeSolver(ctx, solver)
		if err != nil {
			return backendChoice{}, err
		}
		return backendChoice{name: "cuda", grind: solver, estimate: solver, device: result.Device}, nil
	case "webgpu", "wgpu":
		solver := &shasolve.WebGPUSolver{}
		result, err := probeSolver(ctx, solver)
		if err != nil {
			return backendChoice{}, err
		}
		return backendChoice{name: "webgpu", grind: solver, estimate: solver, device: result.Device}, nil
	default:
		return backendChoice{}, fmt.Errorf("unknown solver %q", name)
	}
}

func autoBackend(ctx context.Context) (backendChoice, error) {
	candidates := []backendChoice{
		{name: "metal", grind: shasolve.MetalSolver{}, estimate: shasolve.MetalSolver{}},
		{name: "cuda", grind: shasolve.CUDASolver{}, estimate: shasolve.CUDASolver{}},
		{name: "webgpu", grind: &shasolve.WebGPUSolver{}, estimate: &shasolve.WebGPUSolver{}},
		{name: "cpu", estimate: shasolve.CPUSolver{}, device: "cpu"},
	}

	var errs []string
	for _, candidate := range candidates {
		result, err := probeBackend(ctx, candidate)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", candidate.name, err))
			continue
		}
		candidate.hps = result.HashesPerSecond
		candidate.device = result.Device
		if candidate.device == "" {
			candidate.device = candidate.name
		}
		return candidate, nil
	}
	return backendChoice{}, fmt.Errorf("no usable solver found: %s", strings.Join(errs, "; "))
}

func probeSolver(ctx context.Context, solver shasolve.Solver) (shasolve.Result, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	job := shasolve.DefaultBenchmarkJob(8)
	job.Limit = 1 << 16
	return solver.Solve(probeCtx, job)
}

func probeBackend(ctx context.Context, backend backendChoice) (shasolve.Result, error) {
	if backend.estimate == nil {
		return shasolve.Result{Backend: "cpu", Device: "cpu", HashesPerSecond: 0}, nil
	}
	return probeSolver(ctx, backend.estimate)
}

func measureBackend(ctx context.Context, backend backendChoice, difficulty uint32) (shasolve.Result, error) {
	estimator := backend.estimate
	if estimator == nil {
		estimator = shasolve.CPUSolver{}
	}
	measureCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	job := shasolve.DefaultBenchmarkJob(difficulty)
	return estimator.Solve(measureCtx, job)
}

type candidatePrompter struct {
	backend backendChoice
	reader  *bufio.Reader
	hps     float64
	device  string
}

func newCandidatePrompter(backend backendChoice) *candidatePrompter {
	return &candidatePrompter{
		backend: backend,
		reader:  bufio.NewReader(os.Stdin),
		hps:     backend.hps,
		device:  backend.device,
	}
}

func (p *candidatePrompter) confirm(ctx context.Context, candidate powcoins.ClaimCandidate) (powcoins.ClaimDecision, error) {
	fmt.Printf("\nUTXO %d/%d: %s:%d value=%d sats conf=%d diff=%d csv=%d fee=%d sats output=%d sats\n",
		candidate.Index, candidate.Total, candidate.UTXO.TxID, candidate.UTXO.Vout,
		candidate.UTXO.Value, candidate.Confirmations, candidate.Difficulty,
		candidate.CSV, candidate.Fee, candidate.OutputValue)

	for {
		fmt.Print("[y]es, [e]stimate, [s]kip, [c]ancel: ")
		line, err := p.reader.ReadString('\n')
		if err != nil {
			return powcoins.ClaimDecisionCancel, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes", "":
			return powcoins.ClaimDecisionAccept, nil
		case "s", "skip":
			return powcoins.ClaimDecisionSkip, nil
		case "c", "cancel", "q", "quit":
			return powcoins.ClaimDecisionCancel, nil
		case "e", "estimate":
			if err := p.estimate(ctx, candidate); err != nil {
				fmt.Printf("estimate failed: %v\n", err)
			}
		default:
			fmt.Println("enter y, e, s, or c")
		}
	}
}

func (p *candidatePrompter) estimate(ctx context.Context, candidate powcoins.ClaimCandidate) error {
	result, err := measureBackend(ctx, p.backend, 21)
	if err != nil {
		return err
	}
	p.hps = result.HashesPerSecond
	p.device = result.Device
	expected := math.Ldexp(1, candidate.Difficulty) / p.hps
	fmt.Printf("estimate: backend=%s", p.backend.name)
	if p.device != "" {
		fmt.Printf(" device=%q", p.device)
	}
	fmt.Printf(" measured=%.2f MH/s expected_time=%s at difficulty=%d\n",
		p.hps/1_000_000, formatDuration(time.Duration(expected*float64(time.Second))), candidate.Difficulty)
	return nil
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	if d < time.Minute {
		return d.Round(100 * time.Millisecond).String()
	}
	if d < time.Hour {
		return d.Round(time.Second).String()
	}
	if d < 48*time.Hour {
		return d.Round(time.Minute).String()
	}
	days := d.Hours() / 24
	if days < 365 {
		return fmt.Sprintf("%.1f days", days)
	}
	return fmt.Sprintf("%.1f years", days/365)
}

func peerMessageDetails(msg powcoins.PeerMessage) string {
	if len(msg.Payload) == 0 {
		return ""
	}

	switch msg.Command {
	case wire.CmdReject:
		var reject wire.MsgReject
		if err := reject.BtcDecode(bytes.NewReader(msg.Payload), uint32(wire.ProtocolVersion), wire.BaseEncoding); err == nil {
			return fmt.Sprintf(" cmd=%s code=%s reason=%q hash=%s", reject.Cmd, reject.Code, reject.Reason, reject.Hash)
		}
	case wire.CmdGetData:
		var getData wire.MsgGetData
		if err := getData.BtcDecode(bytes.NewReader(msg.Payload), uint32(wire.ProtocolVersion), wire.WitnessEncoding); err == nil {
			return fmt.Sprintf(" invs=%d", len(getData.InvList))
		}
	case wire.CmdNotFound:
		var notFound wire.MsgNotFound
		if err := notFound.BtcDecode(bytes.NewReader(msg.Payload), uint32(wire.ProtocolVersion), wire.WitnessEncoding); err == nil {
			return fmt.Sprintf(" invs=%d", len(notFound.InvList))
		}
	}

	payload := msg.Payload
	if len(payload) > 32 {
		payload = payload[:32]
	}
	suffix := ""
	if len(msg.Payload) > len(payload) {
		suffix = "..."
	}
	return fmt.Sprintf(" len=%d payload=%s%s", len(msg.Payload), hex.EncodeToString(payload), suffix)
}
