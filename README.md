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
  -max-difficulty 26 \
  -max-pages 40 \
  -feerate 1 \
  -relay-peer inquisition.bitcoin-signet.net:38333
```

The default Esplora endpoint is `https://mempool.space/signet/api`. Public
Esplora instances can cap large faucet UTXO sets, so the CLI scans paged address
history and derives unspent outputs locally instead of relying on
`/address/:address/utxo`.

## Library

The claim builder can be used as a Go package:

```go
result, err := powcoins.BuildClaim(ctx, powcoins.ClaimOptions{
    Address:       "tb1...",
    MaxDifficulty: 26,
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
