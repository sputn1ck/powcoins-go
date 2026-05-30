package powcoins

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/sputn1ck/powcoins-go/shasolve"
)

var ErrClaimCanceled = errors.New("claim canceled")

type ClaimOptions struct {
	Address       string
	EsploraURL    string
	MaxPages      int
	MinDifficulty int
	MaxDifficulty int
	FeeRate       int64
	GrindSolver   shasolve.Solver
	ConfirmUTXO   func(context.Context, ClaimCandidate) (ClaimDecision, error)
	Progress      func(string, ...any)
	GrindProgress func(tries uint64, elapsedSeconds float64, mhps float64)
}

type ClaimDecision int

const (
	ClaimDecisionAccept ClaimDecision = iota
	ClaimDecisionSkip
	ClaimDecisionCancel
)

type ClaimCandidate struct {
	UTXO          UTXO
	Difficulty    int
	Confirmations int64
	CSV           int64
	FeeRate       int64
	Fee           int64
	OutputValue   int64
	Index         int
	Total         int
	score         float64
}

type ClaimResult struct {
	Tx              *wire.MsgTx
	TxHex           string
	TxID            string
	Selected        UTXO
	Difficulty      int
	CSV             int64
	FeeRate         int64
	Fee             int64
	OutputValue     int64
	Grind           GrindResult
	ScannedUTXOSize int
}

func BuildClaim(ctx context.Context, opts ClaimOptions) (ClaimResult, error) {
	if opts.Address == "" {
		return ClaimResult{}, fmt.Errorf("address is required")
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = 40
	}
	if opts.MaxDifficulty <= 0 {
		opts.MaxDifficulty = 35
	}
	if opts.MinDifficulty < 0 {
		return ClaimResult{}, fmt.Errorf("min difficulty must be non-negative")
	}
	if opts.MinDifficulty > opts.MaxDifficulty {
		return ClaimResult{}, fmt.Errorf("min difficulty %d exceeds max difficulty %d", opts.MinDifficulty, opts.MaxDifficulty)
	}

	destination, err := btcutil.DecodeAddress(opts.Address, &chaincfg.SigNetParams)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("decode destination address: %w", err)
	}
	payTo, err := txscript.PayToAddrScript(destination)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("destination script: %w", err)
	}

	scripts, err := FaucetScripts()
	if err != nil {
		return ClaimResult{}, err
	}
	client := NewEsploraClient(opts.EsploraURL)

	logf := func(format string, args ...any) {
		if opts.Progress != nil {
			opts.Progress(format, args...)
		}
	}

	tip, err := client.TipHeight(ctx)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("get tip height: %w", err)
	}
	logf("signet tip height: %d", tip)

	feerate := opts.FeeRate
	if feerate <= 0 {
		feerate = 1
	}
	if feerate < 1 {
		return ClaimResult{}, fmt.Errorf("feerate must be at least 1 sat/vB")
	}

	logf("scanning faucet history via %s, max pages/address=%d", client.BaseURL, opts.MaxPages)
	utxos, err := client.FaucetUTXOs(ctx, scripts, opts.MaxPages)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("scan faucet utxos: %w", err)
	}
	if len(utxos) == 0 {
		return ClaimResult{}, fmt.Errorf("no faucet UTXOs found in scanned history")
	}

	var candidates []ClaimCandidate
	easiest := math.MaxInt
	hardest := 0
	for _, utxo := range utxos {
		if utxo.BlockHeight <= 0 || utxo.BlockHeight > tip {
			continue
		}
		conf := tip - utxo.BlockHeight + 1
		diff := max(utxo.Script.MinDiff, utxo.Script.MaxDiff-int(conf/utxo.Script.Delay))
		easiest = min(easiest, diff)
		hardest = max(hardest, diff)
		if diff < opts.MinDifficulty || diff > opts.MaxDifficulty {
			continue
		}
		csv := int64(utxo.Script.MaxDiff-diff) * utxo.Script.Delay
		if conf < csv {
			continue
		}
		_, fee, outputValue, err := unsignedClaimTx(utxo, payTo, csv, feerate)
		if err != nil {
			logf("skipping %s:%d: %v", utxo.TxID, utxo.Vout, err)
			continue
		}
		score := math.Log2(float64(utxo.Value)) - float64(diff) + rand.Float64()
		candidates = append(candidates, ClaimCandidate{
			UTXO:          utxo,
			Difficulty:    diff,
			Confirmations: conf,
			CSV:           csv,
			FeeRate:       feerate,
			Fee:           fee,
			OutputValue:   outputValue,
			score:         score,
		})
	}
	if len(candidates) == 0 {
		return ClaimResult{}, fmt.Errorf("no faucet UTXO found in difficulty range [%d,%d]; scanned range [%d,%d]",
			opts.MinDifficulty, opts.MaxDifficulty, easiest, hardest)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	for i := range candidates {
		candidates[i].Index = i + 1
		candidates[i].Total = len(candidates)
	}

	selectedCandidate, err := selectClaimCandidate(ctx, candidates, opts.ConfirmUTXO)
	if err != nil {
		return ClaimResult{}, err
	}
	selected := selectedCandidate.UTXO
	selectedDiff := selectedCandidate.Difficulty
	selectedConf := selectedCandidate.Confirmations
	csv := selectedCandidate.CSV

	tx, fee, outputValue, err := unsignedClaimTx(selected, payTo, csv, feerate)
	if err != nil {
		return ClaimResult{}, err
	}

	privKey := privateKeyOne()
	prevFetcher := txscript.NewCannedPrevOutputFetcher(selected.Script.ScriptPubKey, selected.Value)
	sigHashes := txscript.NewTxSigHashes(tx, prevFetcher)
	signature, err := txscript.RawTxInTapscriptSignature(
		tx, sigHashes, 0, selected.Value, selected.Script.ScriptPubKey,
		selected.Script.TapLeaf, txscript.SigHashDefault, privKey,
	)
	if err != nil {
		return ClaimResult{}, fmt.Errorf("sign tapscript spend: %w", err)
	}
	if len(signature) != schnorr.SignatureSize {
		return ClaimResult{}, fmt.Errorf("unexpected schnorr signature length %d", len(signature))
	}
	if selected.Script.HashSig {
		return ClaimResult{}, fmt.Errorf("hashsig faucet scripts are not implemented")
	}

	header := fakeHeader(signature, selectedDiff)
	logf("selected %s:%d value=%d sats conf=%d diff=%d csv=%d fee=%d sats",
		selected.TxID, selected.Vout, selected.Value, selectedConf, selectedDiff, csv, fee)
	logf("grinding fake block header at difficulty %d", selectedDiff)
	grind, err := grindHeader(ctx, header, selectedDiff, opts)
	if err != nil {
		return ClaimResult{}, err
	}

	prefix, suffix, hB, hA, err := splitPowWitness(grind.Header, selectedDiff)
	if err != nil {
		return ClaimResult{}, err
	}
	tx.TxIn[0].Witness = wire.TxWitness{
		signature,
		schnorr.SerializePubKey(privKey.PubKey()),
		prefix,
		suffix,
		hB,
		hA,
		[]byte{byte(selectedDiff)},
		selected.Script.Script,
		selected.Script.ControlBlock,
	}

	txHex, err := txHex(tx)
	if err != nil {
		return ClaimResult{}, err
	}
	txid := tx.TxHash().String()
	return ClaimResult{
		Tx:              tx,
		TxHex:           txHex,
		TxID:            txid,
		Selected:        selected,
		Difficulty:      selectedDiff,
		CSV:             csv,
		FeeRate:         feerate,
		Fee:             fee,
		OutputValue:     outputValue,
		Grind:           grind,
		ScannedUTXOSize: len(utxos),
	}, nil
}

func selectClaimCandidate(ctx context.Context, candidates []ClaimCandidate, confirm func(context.Context, ClaimCandidate) (ClaimDecision, error)) (ClaimCandidate, error) {
	if confirm == nil {
		return candidates[0], nil
	}
	for _, candidate := range candidates {
		decision, err := confirm(ctx, candidate)
		if err != nil {
			return ClaimCandidate{}, err
		}
		switch decision {
		case ClaimDecisionAccept:
			return candidate, nil
		case ClaimDecisionSkip:
			continue
		case ClaimDecisionCancel:
			return ClaimCandidate{}, ErrClaimCanceled
		default:
			return ClaimCandidate{}, fmt.Errorf("unknown claim decision %d", decision)
		}
	}
	return ClaimCandidate{}, fmt.Errorf("no faucet UTXO selected")
}

func grindHeader(ctx context.Context, header []byte, difficulty int, opts ClaimOptions) (GrindResult, error) {
	if opts.GrindSolver == nil {
		return GrindHeader(ctx, header, difficulty, func(tries uint64, elapsed time.Duration) {
			if opts.GrindProgress != nil {
				opts.GrindProgress(tries, elapsed.Seconds(), float64(tries)/max(elapsed.Seconds(), 0.001)/1_000_000)
			}
		})
	}

	if len(header) != 80 {
		return GrindResult{}, fmt.Errorf("header must be 80 bytes, got %d", len(header))
	}
	var fixedHeader [80]byte
	copy(fixedHeader[:], header)
	result, err := opts.GrindSolver.Solve(ctx, shasolve.Job{
		Header:     fixedHeader,
		Difficulty: uint32(difficulty),
	})
	if err != nil {
		return GrindResult{}, err
	}
	if opts.GrindProgress != nil {
		opts.GrindProgress(result.Tries, result.Elapsed.Seconds(), result.HashesPerSecond/1_000_000)
	}
	return GrindResult{
		Header:          append([]byte(nil), result.Header[:]...),
		Hash:            append([]byte(nil), result.Hash[:]...),
		Nonce:           result.Nonce,
		Tries:           result.Tries,
		Elapsed:         result.Elapsed,
		HashesPerSecond: result.HashesPerSecond,
	}, nil
}

func unsignedClaimTx(utxo UTXO, payTo []byte, csv int64, feerate int64) (*wire.MsgTx, int64, int64, error) {
	txid, err := chainhash.NewHashFromStr(utxo.TxID)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parse txid: %w", err)
	}

	tx := wire.NewMsgTx(2)
	tx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  *txid,
			Index: utxo.Vout,
		},
		Sequence: uint32(csv),
	})
	tx.AddTxOut(&wire.TxOut{
		Value:    utxo.Value,
		PkScript: payTo,
	})

	weightBefore := tx.SerializeSizeStripped() * 4
	fee := int64(math.Ceil(float64(weightBefore+int(utxo.Script.ExtraWeight)) / 4 * float64(feerate)))
	if utxo.Value-fee < 10000 {
		return nil, 0, 0, fmt.Errorf("fee %d leaves dust output %d", fee, utxo.Value-fee)
	}
	tx.TxOut[0].Value -= fee
	return tx, fee, tx.TxOut[0].Value, nil
}

func privateKeyOne() *btcec.PrivateKey {
	key := make([]byte, 32)
	key[31] = 1
	priv, _ := btcec.PrivKeyFromBytes(key)
	return priv
}

func txHex(tx *wire.MsgTx) (string, error) {
	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf.Bytes()), nil
}
