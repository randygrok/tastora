package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/chatton/celestia-test/framework/types"
	"io"
	"net/http"
	"strconv"
)

type HeaderResult struct {
	Header Header `json:"header"`
}

type Header struct {
	Height string `json:"height"`
}

type GetHeaderResponse struct {
	Result HeaderResult `json:"result"`
	Error  *RPCError    `json:"error"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (b *BridgeNode) GetHeader(ctx context.Context, height uint64) (types.Header, error) {
	url := fmt.Sprintf("http://%s", b.hostRPCPort)

	// JSON-RPC payload
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "header.GetByHeight",
		"params":  []uint64{height},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return types.Header{}, fmt.Errorf("failed to marshal JSON payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return types.Header{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return types.Header{}, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return types.Header{}, fmt.Errorf("unexpected status code: %s, body: %s", resp.Status, respBody)
	}

	var rpcResp GetHeaderResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return types.Header{}, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	if rpcResp.Error != nil {
		return types.Header{}, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	h, err := strconv.Atoi(rpcResp.Result.Header.Height)
	if err != nil {
		return types.Header{}, fmt.Errorf("failed to parse height: %w", err)
	}

	return types.Header{
		Height: uint64(h),
	}, nil
}
