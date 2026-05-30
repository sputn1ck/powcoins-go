package powcoins

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

type GrindResult struct {
	Header          []byte
	Hash            []byte
	Nonce           uint32
	Tries           uint64
	Elapsed         time.Duration
	HashesPerSecond float64
}

func GrindHeader(ctx context.Context, header []byte, difficulty int, progress func(tries uint64, elapsed time.Duration)) (GrindResult, error) {
	if len(header) != 80 {
		return GrindResult{}, fmt.Errorf("header must be 80 bytes, got %d", len(header))
	}
	if difficulty < 0 || difficulty > 80 {
		return GrindResult{}, fmt.Errorf("unsupported difficulty %d", difficulty)
	}

	started := time.Now()
	nextProgress := started.Add(5 * time.Second)
	var tries uint64
	work := append([]byte(nil), header...)
	for nonce := uint32(0); ; nonce++ {
		binary.LittleEndian.PutUint32(work[76:80], nonce)
		hash := chainhash.DoubleHashB(work)
		tries++
		if validPowHash(hash, difficulty) {
			elapsed := time.Since(started)
			return GrindResult{
				Header:          append([]byte(nil), work...),
				Hash:            hash,
				Nonce:           nonce,
				Tries:           tries,
				Elapsed:         elapsed,
				HashesPerSecond: float64(tries) / max(elapsed.Seconds(), 0.001),
			}, nil
		}
		if nonce == ^uint32(0) {
			return GrindResult{}, fmt.Errorf("nonce space exhausted after %d tries", tries)
		}
		select {
		case <-ctx.Done():
			return GrindResult{}, ctx.Err()
		default:
		}
		if progress != nil && time.Now().After(nextProgress) {
			progress(tries, time.Since(started))
			nextProgress = time.Now().Add(5 * time.Second)
		}
	}
}

func validPowHash(hash []byte, difficulty int) bool {
	fullZeroBytes := difficulty / 8
	partialBits := difficulty % 8
	for i := 0; i < fullZeroBytes; i++ {
		if hash[31-i] != 0 {
			return false
		}
	}
	if partialBits == 0 {
		return true
	}
	next := hash[31-fullZeroBytes]
	return next < byte(1<<(8-partialBits))
}

func fakeHeader(signature []byte, difficulty int) []byte {
	header := make([]byte, 80)
	header[0] = 0x03
	copy(header[4:68], signature)
	header[68] = 0x0b
	exp := byte((280 - difficulty) / 8)
	mant := byte(1 << ((280 - difficulty) % 8))
	header[72] = mant
	header[75] = exp
	return header
}

func splitPowWitness(header []byte, difficulty int) (prefix, suffix, hB, hA []byte, err error) {
	if len(header) != 80 {
		return nil, nil, nil, nil, fmt.Errorf("header must be 80 bytes")
	}
	hash := chainhash.DoubleHashB(header)
	if !validPowHash(hash, difficulty) {
		return nil, nil, nil, nil, fmt.Errorf("header does not satisfy difficulty %d", difficulty)
	}
	n := 32 - difficulty/8 - 1
	if n < 0 || n >= len(hash) {
		return nil, nil, nil, nil, fmt.Errorf("invalid difficulty split %d", difficulty)
	}
	return append([]byte(nil), header[1:4]...),
		append([]byte(nil), header[69:]...),
		append([]byte(nil), hash[:n]...),
		[]byte{hash[n]},
		nil
}

func sha256Bytes(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
