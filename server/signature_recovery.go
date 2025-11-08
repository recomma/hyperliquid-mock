package server

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	hyperliquid "github.com/sonirico/go-hyperliquid"
	"github.com/vmihailenco/msgpack/v5"
)

// recoverWalletFromSignature attempts to recover the wallet address from an /exchange request.
func (h *Handler) recoverWalletFromSignature(req *ExchangeRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("nil exchange request")
	}

	msgHash, err := computeExchangeMessageHash(req)
	if err != nil {
		return "", err
	}

	sigBytes, err := decodeExchangeSignature(req.Signature.R, req.Signature.S, req.Signature.V)
	if err != nil {
		return "", err
	}

	pubKey, err := crypto.SigToPub(msgHash, sigBytes)
	if err != nil {
		return "", fmt.Errorf("failed to recover public key: %w", err)
	}

	return crypto.PubkeyToAddress(*pubKey).Hex(), nil
}

func computeExchangeMessageHash(req *ExchangeRequest) ([]byte, error) {
	action, err := normalizeActionPayload(req.Action)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize action: %w", err)
	}

	actionHash, err := encodeActionHash(action, req.Nonce, req.VaultAddress, req.ExpiresAfter)
	if err != nil {
		return nil, err
	}

	// Test server defaults to testnet behaviour.
	const isMainnet = false

	phantomAgent := constructPhantomAgent(actionHash, isMainnet)
	typedData := l1Payload(phantomAgent, isMainnet)

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, fmt.Errorf("failed to hash domain: %w", err)
	}

	typedHash, err := hashStructLenient(typedData, typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, fmt.Errorf("failed to hash typed data: %w", err)
	}

	raw := []byte{0x19, 0x01}
	raw = append(raw, domainSeparator...)
	raw = append(raw, typedHash...)
	return crypto.Keccak256(raw), nil
}

func normalizeActionPayload(action interface{}) (interface{}, error) {
	if action == nil {
		return nil, fmt.Errorf("missing action payload")
	}

	body, err := json.Marshal(action)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal action: %w", err)
	}

	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("failed to inspect action type: %w", err)
	}

	if probe.Type == "" {
		var raw map[string]interface{}
		if err := json.Unmarshal(body, &raw); err == nil {
			switch {
			case raw["orders"] != nil:
				probe.Type = "order"
			case raw["cancels"] != nil:
				// Determine if cancels use cloinds or oids based on element shape.
				if cancels, ok := raw["cancels"].([]interface{}); ok && len(cancels) > 0 {
					if cancelEntry, _ := cancels[0].(map[string]interface{}); cancelEntry != nil {
						if _, ok := cancelEntry["cloid"]; ok {
							probe.Type = "cancelByCloid"
						} else {
							probe.Type = "cancel"
						}
					}
				}
			case raw["modifies"] != nil:
				probe.Type = "batchModify"
			case raw["order"] != nil:
				probe.Type = "modify"
			}
		}
	}

	switch probe.Type {
	case "order":
		var typed hyperliquid.OrderAction
		if err := json.Unmarshal(body, &typed); err != nil {
			return nil, fmt.Errorf("failed to decode order action: %w", err)
		}
		return typed, nil
	case "modify":
		var typed hyperliquid.ModifyAction
		if err := json.Unmarshal(body, &typed); err != nil {
			return nil, fmt.Errorf("failed to decode modify action: %w", err)
		}
		return typed, nil
	case "batchModify":
		var typed hyperliquid.BatchModifyAction
		if err := json.Unmarshal(body, &typed); err != nil {
			return nil, fmt.Errorf("failed to decode batchModify action: %w", err)
		}
		return typed, nil
	case "cancel":
		var typed hyperliquid.CancelAction
		if err := json.Unmarshal(body, &typed); err != nil {
			return nil, fmt.Errorf("failed to decode cancel action: %w", err)
		}
		return typed, nil
	case "cancelByCloid":
		var typed hyperliquid.CancelByCloidAction
		if err := json.Unmarshal(body, &typed); err != nil {
			return nil, fmt.Errorf("failed to decode cancelByCloid action: %w", err)
		}
		return typed, nil
	default:
		return nil, fmt.Errorf("unsupported action type %q", probe.Type)
	}
}

func encodeActionHash(action interface{}, nonce int64, vaultAddress *string, expiresAfter *int64) ([]byte, error) {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.UseCompactInts(true)

	if err := enc.Encode(action); err != nil {
		return nil, fmt.Errorf("failed to msgpack encode action: %w", err)
	}
	data := convertStr16ToStr8(buf.Bytes())

	if nonce < 0 {
		return nil, fmt.Errorf("nonce cannot be negative: %d", nonce)
	}
	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, uint64(nonce))
	data = append(data, nonceBytes...)

	vault := ""
	if vaultAddress != nil {
		vault = *vaultAddress
	}

	if vault == "" {
		data = append(data, 0x00)
	} else {
		addrBytes, err := addressToBytes(vault)
		if err != nil {
			return nil, err
		}
		data = append(data, 0x01)
		data = append(data, addrBytes...)
	}

	if expiresAfter != nil {
		if *expiresAfter < 0 {
			return nil, fmt.Errorf("expiresAfter cannot be negative: %d", *expiresAfter)
		}
		data = append(data, 0x00)
		expBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(expBytes, uint64(*expiresAfter))
		data = append(data, expBytes...)
	}

	return crypto.Keccak256(data), nil
}

func convertStr16ToStr8(data []byte) []byte {
	result := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		b := data[i]
		if b == 0xda && i+2 < len(data) {
			length := (int(data[i+1]) << 8) | int(data[i+2])
			if length < 256 && i+3+length <= len(data) {
				result = append(result, 0xd9, byte(length))
				i += 3
				result = append(result, data[i:i+length]...)
				i += length
				continue
			}
		}
		result = append(result, b)
		i++
	}
	return result
}

func addressToBytes(address string) ([]byte, error) {
	address = strings.TrimPrefix(strings.TrimPrefix(address, "0x"), "0X")
	if len(address)%2 == 1 {
		address = "0" + address
	}

	bytes, err := hex.DecodeString(address)
	if err != nil {
		return nil, fmt.Errorf("invalid vault address: %w", err)
	}
	if len(bytes) != 20 {
		return nil, fmt.Errorf("vault address must be 20 bytes, got %d", len(bytes))
	}

	return bytes, nil
}

func constructPhantomAgent(hash []byte, isMainnet bool) map[string]interface{} {
	source := "b"
	if isMainnet {
		source = "a"
	}
	return map[string]interface{}{
		"source":       source,
		"connectionId": hash,
	}
}

func l1Payload(phantomAgent map[string]interface{}, isMainnet bool) apitypes.TypedData {
	chainID := math.HexOrDecimal256(*big.NewInt(1337))
	return apitypes.TypedData{
		Domain: apitypes.TypedDataDomain{
			ChainId:           &chainID,
			Name:              "Exchange",
			Version:           "1",
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Types: apitypes.Types{
			"Agent": []apitypes.Type{
				{Name: "source", Type: "string"},
				{Name: "connectionId", Type: "bytes32"},
			},
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
		},
		PrimaryType: "Agent",
		Message:     phantomAgent,
	}
}

func hashStructLenient(typedData apitypes.TypedData, primaryType string, message map[string]interface{}) ([]byte, error) {
	types := typedData.Types[primaryType]
	filtered := make(map[string]interface{}, len(types))
	for _, t := range types {
		if val, ok := message[t.Name]; ok {
			filtered[t.Name] = val
		}
	}
	return typedData.HashStruct(primaryType, filtered)
}

func decodeExchangeSignature(rHex, sHex string, v int) ([]byte, error) {
	rBytes, err := decodeSignatureComponent(rHex)
	if err != nil {
		return nil, fmt.Errorf("invalid signature R: %w", err)
	}
	sBytes, err := decodeSignatureComponent(sHex)
	if err != nil {
		return nil, fmt.Errorf("invalid signature S: %w", err)
	}

	signature := make([]byte, 65)
	copy(signature[32-len(rBytes):32], rBytes)
	copy(signature[64-len(sBytes):64], sBytes)

	switch v {
	case 27, 28:
		signature[64] = byte(v - 27)
	case 0, 1:
		signature[64] = byte(v)
	default:
		return nil, fmt.Errorf("invalid signature recovery id: %d", v)
	}

	return signature, nil
}

func decodeSignatureComponent(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("empty value")
	}

	component := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(component)%2 == 1 {
		component = "0" + component
	}

	bytes, err := hex.DecodeString(component)
	if err != nil {
		return nil, err
	}
	if len(bytes) > 32 {
		return nil, fmt.Errorf("component too large (%d bytes)", len(bytes))
	}
	return bytes, nil
}
