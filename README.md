# powcoins-go

Go CLI and library for claiming coins from
[`ajtowns/powcoins`](https://github.com/ajtowns/powcoins) on Bitcoin Signet.

It scans Signet Esplora address history for faucet UTXOs, builds the Taproot
script-path claim transaction, CPU-grinds the fake block header proof, and
relays the transaction directly to the Bitcoin Inquisition Signet peer over P2P.

## Usage

Build the CLI:

```sh
go build -o powcoins-go ./cmd/powcoins-go
```

Dry-run without relaying:

```sh
./powcoins-go -address <signet-address> -dry-run
```

Claim and relay:

```sh
./powcoins-go -address <signet-address>
```

Useful options:

```sh
./powcoins-go \
  -address <signet-address> \
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

## Library

The claim builder can be used as a Go package:

```go
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
