package proxy

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ResolveGhostSessionID returns client sessionID or a deterministic hex UUID derived from keyHash
// used strictly for Redis blob storage, HKDF key derivation, and read memoization.
func ResolveGhostSessionID(headerSessionID, keyHash string) string {
	if headerSessionID != "" {
		return headerSessionID
	}
	sum := sha256.Sum256([]byte(keyHash))
	hexStr := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}

// ContentBlobID generates a deterministic 16-character hex identifier (8 bytes) based on SHA-256 of the payload.
func ContentBlobID(rawResult []byte) string {
	sum := sha256.Sum256(rawResult)
	return hex.EncodeToString(sum[:8])
}

// GenerateBlobID generates a random 16-character hex identifier (8 bytes entropy).
func GenerateBlobID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

var blobRefRegex = regexp.MustCompile(`loopers:blob:([0-9a-fA-F]{16})`)

// GhostStub represents the compact summary replacing offloaded payload in tool results.
type GhostStub struct {
	Ref            string      `json:"_loopers_ref"`
	Tool           string      `json:"tool"`
	OriginalBytes  int         `json:"original_bytes"`
	EstTokensSaved int         `json:"est_tokens_saved"`
	Structure      interface{} `json:"structure,omitempty"`
	Preview        interface{} `json:"preview,omitempty"`
}

// hkdfExtract implements RFC 5869 extract step using HMAC-SHA256.
func hkdfExtract(salt, ikm []byte) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	h := hmac.New(sha256.New, salt)
	h.Write(ikm)
	return h.Sum(nil)
}

// hkdfExpand implements RFC 5869 expand step using HMAC-SHA256.
func hkdfExpand(prk, info []byte, length int) []byte {
	h := hmac.New(sha256.New, prk)
	var okm []byte
	var t []byte
	counter := byte(1)
	for len(okm) < length {
		h.Reset()
		h.Write(t)
		h.Write(info)
		h.Write([]byte{counter})
		t = h.Sum(nil)
		okm = append(okm, t...)
		counter++
	}
	return okm[:length]
}

// DeriveGhostKey derives an ephemeral 256-bit AES key from proxyKey and sessionID using HKDF-SHA256.
func DeriveGhostKey(proxyKey, sessionID string) []byte {
	prk := hkdfExtract([]byte(sessionID), []byte(proxyKey))
	return hkdfExpand(prk, []byte("loopers-ghost-v1"), 32)
}

// EncryptGhostBlob encrypts plaintext using AES-256-GCM.
func EncryptGhostBlob(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptGhostBlob decrypts ciphertext using AES-256-GCM.
func DecryptGhostBlob(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, nil)
}

// CreateGhostStub constructs a compact JSON Ghost Stub from a raw tool payload.
func CreateGhostStub(toolName string, rawResult []byte, blobID string) ([]byte, error) {
	originalBytes := len(rawResult)
	estTokensSaved := originalBytes / 4

	stub := GhostStub{
		Ref:            fmt.Sprintf("loopers:blob:%s", blobID),
		Tool:           toolName,
		OriginalBytes:  originalBytes,
		EstTokensSaved: estTokensSaved,
	}

	// Try parsing as JSON for structural inspection
	var jsonVal interface{}
	if err := json.Unmarshal(rawResult, &jsonVal); err == nil {
		switch v := jsonVal.(type) {
		case []interface{}:
			var keys []string
			if len(v) > 0 {
				if firstMap, ok := v[0].(map[string]interface{}); ok {
					for k := range firstMap {
						keys = append(keys, k)
					}
					sort.Strings(keys)
				}
			}
			stub.Structure = map[string]interface{}{
				"rows": len(v),
				"keys": keys,
			}
			if len(v) > 0 {
				stub.Preview = v[:1]
			}
		case map[string]interface{}:
			var keys []string
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			stub.Structure = map[string]interface{}{
				"keys": keys,
			}
			previewMap := make(map[string]interface{})
			for i := 0; i < len(keys) && i < 3; i++ {
				k := keys[i]
				previewMap[k] = v[k]
			}
			stub.Preview = previewMap
		default:
			stub.Structure = "json_primitive"
		}
	} else {
		// Plain text / logs summary
		lines := strings.Split(string(rawResult), "\n")
		stub.Structure = map[string]interface{}{
			"lines": len(lines),
		}
		maxLines := 5
		if len(lines) < maxLines {
			maxLines = len(lines)
		}
		var previewLines []string
		for _, l := range lines[:maxLines] {
			if len(l) > 256 {
				l = l[:256] + "..."
			}
			previewLines = append(previewLines, l)
		}
		stub.Preview = previewLines
	}

	return json.Marshal(stub)
}

// HydratePromptTail inspects the prompt body for Ghost Token references and appends hydrated
// context blocks to the final message (messages[len-1]) while leaving historical turns (messages[0..len-2])
// 100% untouched to preserve upstream byte-level KV-caches.
func HydratePromptTail(
	ctx context.Context,
	keyHash, proxyKey, sessionID string,
	body []byte,
	rdb *redis.Client,
	isFrontierModel bool,
	maxHydrationTokens int,
) ([]byte, error) {
	if len(body) == 0 || !isFrontierModel || rdb == nil || proxyKey == "" || sessionID == "" {
		return body, nil
	}

	matches := blobRefRegex.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return body, nil
	}

	// Deduplicate referenced blob IDs
	uniqueBlobIDs := make(map[string]bool)
	var orderedBlobIDs []string
	for _, m := range matches {
		if len(m) > 1 {
			id := string(m[1])
			if !uniqueBlobIDs[id] {
				uniqueBlobIDs[id] = true
				orderedBlobIDs = append(orderedBlobIDs, id)
			}
		}
	}

	if len(orderedBlobIDs) == 0 {
		return body, nil
	}

	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, nil
	}

	messagesRaw, ok := reqMap["messages"].([]interface{})
	if !ok || len(messagesRaw) == 0 {
		return body, nil
	}

	// Validate tail message role BEFORE performing Redis GETs and AES-GCM decryption.
	// Never auto-hydrate onto an intermediate tool-return message (role == "tool"
	// or Anthropic tool_result content blocks). Hydration is strictly reserved for
	// user-initiated synthesis turns so tool-chaining loops stay compact.
	lastIdx := len(messagesRaw) - 1
	lastMsg, ok := messagesRaw[lastIdx].(map[string]interface{})
	if !ok {
		return body, nil
	}
	if role, _ := lastMsg["role"].(string); role != "user" {
		return body, nil
	}
	if blocks, ok := lastMsg["content"].([]interface{}); ok {
		for _, b := range blocks {
			if bMap, ok := b.(map[string]interface{}); ok {
				if bType, _ := bMap["type"].(string); bType == "tool_result" {
					return body, nil
				}
			}
		}
	}

	ghostKey := DeriveGhostKey(proxyKey, sessionID)

	// Fetch and decrypt blobs
	var hydratedBlocks []string
	totalTokensEstimated := 0
	if maxHydrationTokens <= 0 {
		maxHydrationTokens = 16000
	}

	for _, blobID := range orderedBlobIDs {
		redisKey := fmt.Sprintf("loopers:blob:{%s}:%s:%s", keyHash, sessionID, blobID)
		encPayload, err := rdb.Get(ctx, redisKey).Bytes()
		if err != nil || len(encPayload) == 0 {
			continue
		}

		decrypted, decErr := DecryptGhostBlob(ghostKey, encPayload)
		if decErr != nil {
			continue
		}

		estTokens := len(decrypted) / 4
		if totalTokensEstimated+estTokens > maxHydrationTokens {
			// Cap payload to stay within maximum hydration token budget
			maxChars := (maxHydrationTokens - totalTokensEstimated) * 4
			if maxChars > 0 && maxChars < len(decrypted) {
				decrypted = append(decrypted[:maxChars], []byte("\n... [hydrated payload truncated by Loopers budget]")...)
			}
		}
		totalTokensEstimated += len(decrypted) / 4

		var blockBuilder strings.Builder
		blockBuilder.WriteString("<loopers_hydrated_context ref=\"loopers:blob:")
		for _, r := range blobID {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				blockBuilder.WriteRune(r)
			}
		}
		blockBuilder.WriteString("\">\n")
		blockBuilder.Write(decrypted)
		blockBuilder.WriteString("\n</loopers_hydrated_context>")
		block := blockBuilder.String()
		hydratedBlocks = append(hydratedBlocks, block)

		if totalTokensEstimated >= maxHydrationTokens {
			break
		}
	}

	if len(hydratedBlocks) == 0 {
		return body, nil
	}

	concatenatedHydration := "\n\n" + strings.Join(hydratedBlocks, "\n\n")

	switch content := lastMsg["content"].(type) {
	case string:
		lastMsg["content"] = content + concatenatedHydration
	case []interface{}:
		// Multimodal or block-level array (Anthropic / OpenAI tool results)
		textBlock := map[string]interface{}{
			"type": "text",
			"text": concatenatedHydration,
		}
		lastMsg["content"] = append(content, textBlock)
	default:
		// If content is empty or unexpected type, set as string
		lastMsg["content"] = concatenatedHydration
	}

	messagesRaw[lastIdx] = lastMsg
	reqMap["messages"] = messagesRaw

	return json.Marshal(reqMap)
}
