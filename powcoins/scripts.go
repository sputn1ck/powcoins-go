package powcoins

import (
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

const (
	DefaultEsploraURL = "https://mempool.space/signet/api"
	DefaultRelayPeer  = "inquisition.bitcoin-signet.net:38333"
)

var internalKeyXOnly = mustHex("4a981eda2acd9bf5d789fa1a9b7f95677776d99fd13ec0f658f0e4dbc5b5c878")

type FaucetScript struct {
	Delay       int64
	MinDiff     int
	MaxDiff     int
	ExtraWeight int64
	HashSig     bool
	Script      []byte

	Address       string
	ScriptPubKey  []byte
	TapLeaf       txscript.TapLeaf
	ControlBlock  []byte
	InternalKey   *btcec.PublicKey
	TaprootOutput *btcec.PublicKey
}

func FaucetScripts() ([]FaucetScript, error) {
	specs := []struct {
		delay       int64
		minDiff     int
		maxDiff     int
		extraWeight int64
		hashSig     bool
		scriptHex   string
	}{
		{10, 16, 80, 360, false, "60947600a2697601409f697676937676937693930280027c94b2750200006b760120a2636c04000000007e6b012094687660a2636c0200007e6b6094687658a2636c01007e6b5894687c825188766b517e020001947c7600876302000167765187630280006776528763014067765387630120677654876360677655876358677656876354675268686868686868779f696c6c7e7e6b708201208875820140887c827c7e7e7c827c7e7c7eaa6c88ac"},
		{4, 16, 80, 356, false, "60947600a2697601409f6976769376930200017c94b2750200006b760120a2636c04000000007e6b012094687660a2636c0200007e6b6094687658a2636c01007e6b5894687c825188766b517e020001947c7600876302000167765187630280006776528763014067765387630120677654876360677655876358677656876354675268686868686868779f696c6c7e7e6b708201208875820140887c827c7e7e7c827c7e7c7eaa6c88ac"},
		{1, 16, 80, 351, false, "60947600a2697601409f697601407c94b2750200006b760120a2636c04000000007e6b012094687660a2636c0200007e6b6094687658a2636c01007e6b5894687c825188766b517e020001947c7600876302000167765187630280006776528763014067765387630120677654876360677655876358677656876354675268686868686868779f696c6c7e7e6b708201208875820140887c827c7e7e7c827c7e7c7eaa6c88ac"},
	}

	internalKey, err := btcec.ParsePubKey(append([]byte{0x02}, internalKeyXOnly...))
	if err != nil {
		return nil, fmt.Errorf("parse internal key: %w", err)
	}

	scripts := make([]FaucetScript, 0, len(specs))
	for _, spec := range specs {
		script, err := hex.DecodeString(spec.scriptHex)
		if err != nil {
			return nil, fmt.Errorf("decode faucet script: %w", err)
		}

		leaf := txscript.NewBaseTapLeaf(script)
		tree := txscript.AssembleTaprootScriptTree(leaf)
		rootHash := tree.RootNode.TapHash()
		outputKey := txscript.ComputeTaprootOutputKey(internalKey, rootHash[:])
		spk, err := txscript.PayToTaprootScript(outputKey)
		if err != nil {
			return nil, fmt.Errorf("derive taproot script: %w", err)
		}

		addr, err := btcutil.NewAddressTaproot(
			schnorr.SerializePubKey(outputKey), &chaincfg.SigNetParams,
		)
		if err != nil {
			return nil, fmt.Errorf("derive taproot address: %w", err)
		}

		ctrl := tree.LeafMerkleProofs[0].ToControlBlock(internalKey)
		ctrlBytes, err := ctrl.ToBytes()
		if err != nil {
			return nil, fmt.Errorf("serialize control block: %w", err)
		}

		scripts = append(scripts, FaucetScript{
			Delay:         spec.delay,
			MinDiff:       spec.minDiff,
			MaxDiff:       spec.maxDiff,
			ExtraWeight:   spec.extraWeight,
			HashSig:       spec.hashSig,
			Script:        script,
			Address:       addr.EncodeAddress(),
			ScriptPubKey:  spk,
			TapLeaf:       leaf,
			ControlBlock:  ctrlBytes,
			InternalKey:   internalKey,
			TaprootOutput: outputKey,
		})
	}

	return scripts, nil
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
