package shasolve

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/bits"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

const (
	NotFound         = ^uint32(0)
	DefaultBatchSize = 65535 * 256
)

type Job struct {
	Header     [80]byte
	Difficulty uint32
	StartNonce uint32
	Limit      uint64
	BatchSize  uint32
}

type Result struct {
	Header          [80]byte
	Hash            [32]byte
	Nonce           uint32
	Tries           uint64
	Elapsed         time.Duration
	HashesPerSecond float64
	ZeroBits        uint32
	Backend         string
	Device          string
}

type Solver interface {
	Name() string
	Solve(context.Context, Job) (Result, error)
}

func NewPowcoinHeader(signature [64]byte, difficulty uint32) ([80]byte, error) {
	if difficulty > 80 {
		return [80]byte{}, fmt.Errorf("powcoin difficulty must be <= 80, got %d", difficulty)
	}
	var header [80]byte
	header[0] = 0x03
	copy(header[4:68], signature[:])
	header[68] = 0x0b
	exp := byte((280 - difficulty) / 8)
	mant := byte(1 << ((280 - difficulty) % 8))
	header[72] = mant
	header[75] = exp
	return header, nil
}

func BenchmarkSignature() [64]byte {
	var sig [64]byte
	seed := chainhash.DoubleHashB([]byte("github.com/sputn1ck/webgpu-sha powcoin benchmark signature v1"))
	copy(sig[:32], seed)
	seed2 := chainhash.DoubleHashB(seed)
	copy(sig[32:], seed2)
	return sig
}

func DefaultBenchmarkJob(difficulty uint32) Job {
	header, err := NewPowcoinHeader(BenchmarkSignature(), difficulty)
	if err != nil {
		panic(err)
	}
	return Job{
		Header:     header,
		Difficulty: difficulty,
		BatchSize:  DefaultBatchSize,
	}
}

func Difficulty28BenchmarkJob() Job {
	job := DefaultBenchmarkJob(28)
	job.StartNonce = 0
	job.Limit = 0
	return job
}

func NormalizeJob(job Job) Job {
	if job.BatchSize == 0 {
		job.BatchSize = DefaultBatchSize
	}
	return job
}

func HeaderHash(header [80]byte, nonce uint32) [32]byte {
	binary.LittleEndian.PutUint32(header[76:80], nonce)
	hash := chainhash.DoubleHashB(header[:])
	var out [32]byte
	copy(out[:], hash)
	return out
}

func TrailingZeroBits(hash [32]byte) uint32 {
	var count uint32
	for i := len(hash) - 1; i >= 0; i-- {
		b := hash[i]
		if b == 0 {
			count += 8
			if i == 0 {
				return count
			}
			continue
		}
		count += uint32(bits.TrailingZeros8(b))
		return count
	}
	return count
}

func Verify(job Job, nonce uint32) (uint32, bool) {
	hash := HeaderHash(job.Header, nonce)
	zeros := TrailingZeroBits(hash)
	return zeros, zeros >= job.Difficulty
}

func ResultHeader(job Job, nonce uint32) [80]byte {
	header := job.Header
	binary.LittleEndian.PutUint32(header[76:80], nonce)
	return header
}

func result(backend, device string, job Job, nonce uint32, tries uint64, started time.Time) Result {
	elapsed := time.Since(started)
	header := ResultHeader(job, nonce)
	hash := HeaderHash(job.Header, nonce)
	zeros := TrailingZeroBits(hash)
	return Result{
		Header:          header,
		Hash:            hash,
		Nonce:           nonce,
		Tries:           tries,
		Elapsed:         elapsed,
		HashesPerSecond: float64(tries) / max(elapsed.Seconds(), 0.001),
		ZeroBits:        zeros,
		Backend:         backend,
		Device:          device,
	}
}

func checkDifficulty(difficulty uint32) error {
	if difficulty > 80 {
		return fmt.Errorf("powcoin difficulty must be <= 80, got %d", difficulty)
	}
	return nil
}
