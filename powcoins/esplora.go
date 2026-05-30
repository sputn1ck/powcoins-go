package powcoins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type EsploraClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

type UTXO struct {
	TxID        string
	Vout        uint32
	Value       int64
	BlockHeight int64
	Script      FaucetScript
}

type esploraTx struct {
	TxID   string `json:"txid"`
	Status struct {
		Confirmed   bool  `json:"confirmed"`
		BlockHeight int64 `json:"block_height"`
	} `json:"status"`
	Vin []struct {
		TxID    string `json:"txid"`
		Vout    uint32 `json:"vout"`
		Prevout *struct {
			ScriptPubKey string `json:"scriptpubkey"`
		} `json:"prevout"`
	} `json:"vin"`
	Vout []struct {
		ScriptPubKey string `json:"scriptpubkey"`
		Value        int64  `json:"value"`
	} `json:"vout"`
}

func NewEsploraClient(baseURL string) *EsploraClient {
	if baseURL == "" {
		baseURL = DefaultEsploraURL
	}
	return &EsploraClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *EsploraClient) TipHeight(ctx context.Context) (int64, error) {
	body, err := c.get(ctx, "/blocks/tip/height")
	if err != nil {
		return 0, err
	}
	height, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse tip height: %w", err)
	}
	return height, nil
}

func (c *EsploraClient) FeeRate(ctx context.Context) (int64, error) {
	body, err := c.get(ctx, "/fee-estimates")
	if err != nil {
		return 0, err
	}
	var fees map[string]float64
	if err := json.Unmarshal(body, &fees); err != nil {
		return 0, fmt.Errorf("decode fee estimates: %w", err)
	}
	feerate := int64(fees["1"] + 0.999999)
	if feerate < 1 {
		feerate = 1
	}
	return feerate, nil
}

func (c *EsploraClient) FaucetUTXOs(ctx context.Context, scripts []FaucetScript, maxPages int) ([]UTXO, error) {
	bySPK := make(map[string]FaucetScript, len(scripts))
	for _, script := range scripts {
		bySPK[fmt.Sprintf("%x", script.ScriptPubKey)] = script
	}

	type outpoint struct {
		txid string
		vout uint32
	}
	funded := make(map[outpoint]UTXO)
	spent := make(map[outpoint]struct{})

	for _, script := range scripts {
		var lastSeen string
		for page := 0; page < maxPages; page++ {
			path := "/address/" + script.Address + "/txs/chain"
			if lastSeen != "" {
				path += "/" + lastSeen
			}

			body, err := c.get(ctx, path)
			if err != nil {
				return nil, fmt.Errorf("scan %s page %d: %w", script.Address, page+1, err)
			}

			var txs []esploraTx
			if err := json.Unmarshal(body, &txs); err != nil {
				return nil, fmt.Errorf("decode tx page: %w", err)
			}
			if len(txs) == 0 {
				break
			}

			for _, tx := range txs {
				for _, in := range tx.Vin {
					if in.Prevout == nil {
						continue
					}
					if _, ok := bySPK[in.Prevout.ScriptPubKey]; ok {
						spent[outpoint{txid: in.TxID, vout: in.Vout}] = struct{}{}
					}
				}
				if !tx.Status.Confirmed {
					continue
				}
				for vout, out := range tx.Vout {
					faucetScript, ok := bySPK[out.ScriptPubKey]
					if !ok {
						continue
					}
					op := outpoint{txid: tx.TxID, vout: uint32(vout)}
					funded[op] = UTXO{
						TxID:        tx.TxID,
						Vout:        uint32(vout),
						Value:       out.Value,
						BlockHeight: tx.Status.BlockHeight,
						Script:      faucetScript,
					}
				}
			}

			if len(txs) < 25 {
				break
			}
			lastSeen = txs[len(txs)-1].TxID
		}
	}

	utxos := make([]UTXO, 0, len(funded))
	for op, utxo := range funded {
		if _, ok := spent[op]; ok {
			continue
		}
		utxos = append(utxos, utxo)
	}
	return utxos, nil
}

func (c *EsploraClient) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}
