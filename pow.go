package faucetpow

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/bits"
	"strconv"
	"time"
)

const NotFound = ^uint32(0)

type Solution struct {
	Nonce           uint32
	NonceString     string
	Tries           uint64
	Elapsed         time.Duration
	HashesPerSecond float64
	LeadingZeroBits uint32
	Device          string
}

type Solver interface {
	Solve(ctx context.Context, challenge string, difficulty uint32) (Solution, error)
}

type CPUSolver struct{}

func (CPUSolver) Solve(ctx context.Context, challenge string, difficulty uint32) (Solution, error) {
	started := time.Now()
	for nonce := uint32(0); ; nonce++ {
		if err := ctx.Err(); err != nil {
			return Solution{}, err
		}
		nonceString := strconv.FormatUint(uint64(nonce), 10)
		zeros := LeadingZeroBits(DigestPoW(challenge, nonceString))
		if zeros >= difficulty {
			return solution(challenge, nonce, started), nil
		}
		if nonce == NotFound {
			return Solution{}, fmt.Errorf("searched entire u32 nonce space")
		}
	}
}

func DigestPoW(challenge, nonce string) [32]byte {
	h := sha256.New()
	h.Write([]byte(challenge))
	h.Write([]byte{':'})
	h.Write([]byte(nonce))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func LeadingZeroBits(digest [32]byte) uint32 {
	var count uint32
	for _, b := range digest {
		if b == 0 {
			count += 8
			continue
		}
		count += uint32(bits.LeadingZeros8(b))
		return count
	}
	return count
}

func Verify(challenge, nonce string, difficulty uint32) (uint32, bool) {
	zeros := LeadingZeroBits(DigestPoW(challenge, nonce))
	return zeros, zeros >= difficulty
}

func solution(challenge string, nonce uint32, started time.Time) Solution {
	elapsed := time.Since(started)
	nonceString := strconv.FormatUint(uint64(nonce), 10)
	zeros := LeadingZeroBits(DigestPoW(challenge, nonceString))
	tries := uint64(nonce) + 1
	hps := float64(tries) / max(elapsed.Seconds(), 0.001)
	return Solution{
		Nonce:           nonce,
		NonceString:     nonceString,
		Tries:           tries,
		Elapsed:         elapsed,
		HashesPerSecond: hps,
		LeadingZeroBits: zeros,
	}
}

func paramsBytes(prefixLen, startNonce, count, difficulty uint32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], prefixLen)
	binary.LittleEndian.PutUint32(buf[4:8], startNonce)
	binary.LittleEndian.PutUint32(buf[8:12], count)
	binary.LittleEndian.PutUint32(buf[12:16], difficulty)
	return buf
}
