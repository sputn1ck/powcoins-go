package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/btcsuite/btcd/wire"
	"github.com/sputn1ck/webgpu-sha/powcoins"
)

func main() {
	var feeRate int64
	var (
		address      = flag.String("address", "", "signet address to receive claimed coins")
		esplora      = flag.String("esplora", powcoins.DefaultEsploraURL, "signet Esplora API base URL")
		relayPeer    = flag.String("relay-peer", powcoins.DefaultRelayPeer, "signet P2P peer that accepts OP_CAT spends")
		maxPages     = flag.Int("max-pages", 40, "maximum Esplora history pages to scan per faucet address")
		minDiff      = flag.Int("min-difficulty", 0, "minimum PoW difficulty to select; useful to avoid highly contested low-difficulty UTXOs")
		maxDiff      = flag.Int("max-difficulty", 26, "maximum PoW difficulty to attempt")
		dryRun       = flag.Bool("dry-run", false, "build and mine the transaction without relaying it")
		relayTimeout = flag.Duration("relay-timeout", 30*time.Second, "P2P relay timeout")
		peerMessages = flag.Bool("peer-messages", false, "print P2P messages during relay")
		peerListen   = flag.Duration("peer-listen", 0, "after sending the tx, keep reading peer messages for this duration")
	)
	flag.Int64Var(&feeRate, "feerate", 0, "fee rate override in sat/vB; default is 1")
	flag.Int64Var(&feeRate, "fee-rate", 0, "alias for -feerate")
	flag.Parse()

	if *address == "" {
		log.Fatal("-address is required")
	}

	ctx := context.Background()
	result, err := powcoins.BuildClaim(ctx, powcoins.ClaimOptions{
		Address:       *address,
		EsploraURL:    *esplora,
		MaxPages:      *maxPages,
		MinDifficulty: *minDiff,
		MaxDifficulty: *maxDiff,
		FeeRate:       feeRate,
		Progress: func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		},
		GrindProgress: func(tries uint64, elapsedSeconds float64, mhps float64) {
			fmt.Printf("grind: tries=%d elapsed=%.1fs rate=%.2f MH/s\n", tries, elapsedSeconds, mhps)
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("mined: nonce=%d tries=%d elapsed=%.3fs rate=%.2f MH/s\n",
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
