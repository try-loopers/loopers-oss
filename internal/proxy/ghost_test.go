package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestGhostKeyDerivation_Deterministic(t *testing.T) {
	key1 := DeriveGhostKey("lp-test-proxy-key", "sess-123")
	key2 := DeriveGhostKey("lp-test-proxy-key", "sess-123")
	keyDiff := DeriveGhostKey("lp-test-proxy-key", "sess-456")

	assert.Equal(t, 32, len(key1))
	assert.Equal(t, key1, key2)
	assert.NotEqual(t, key1, keyDiff)
}

func TestGhostBlob_EncryptDecrypt_Roundtrip(t *testing.T) {
	key := DeriveGhostKey("lp-secret-key", "session-abc")
	plaintext := []byte("Sensitive customer database export: id=1, name=Alice, email=alice@example.com")

	ciphertext, err := EncryptGhostBlob(key, plaintext)
	assert.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := DecryptGhostBlob(key, ciphertext)
	assert.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	// Wrong key should fail
	wrongKey := DeriveGhostKey("lp-wrong-key", "session-abc")
	_, err = DecryptGhostBlob(wrongKey, ciphertext)
	assert.Error(t, err)
}

func TestCreateGhostStub_JSONAndText(t *testing.T) {
	// 1. JSON Array
	jsonPayload := []byte(`[{"id": 1, "name": "prod-db", "status": "active"}]`)
	stubJSON, err := CreateGhostStub("list_databases", jsonPayload, "abcdef1234567890")
	assert.NoError(t, err)

	var stub GhostStub
	err = json.Unmarshal(stubJSON, &stub)
	assert.NoError(t, err)
	assert.Equal(t, "loopers:blob:abcdef1234567890", stub.Ref)
	assert.Equal(t, "list_databases", stub.Tool)
	assert.Equal(t, len(jsonPayload), stub.OriginalBytes)

	// 2. Plain Text
	textPayload := []byte("Line 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6")
	stubTextJSON, err := CreateGhostStub("fetch_logs", textPayload, "1234567890abcdef")
	assert.NoError(t, err)

	var textStub GhostStub
	err = json.Unmarshal(stubTextJSON, &textStub)
	assert.NoError(t, err)
	assert.Equal(t, "loopers:blob:1234567890abcdef", textStub.Ref)
	assert.Equal(t, "fetch_logs", textStub.Tool)
}

func TestHydratePromptTail_AppendOnly(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	proxyKey := "lp-production-token"
	sessionID := "session-tail-hydration"
	keyHash := "test-key-hash"
	blobID := "a1b2c3d4e5f60718"

	rawSecretData := []byte("SECRET-INTERNAL-METRICS-REPORT: Q3 Revenue +42%, Operating Margin 31%")
	ghostKey := DeriveGhostKey(proxyKey, sessionID)
	encData, err := EncryptGhostBlob(ghostKey, rawSecretData)
	assert.NoError(t, err)

	redisKey := fmt.Sprintf("loopers:blob:{%s}:%s:%s", keyHash, sessionID, blobID)
	err = rdb.Set(context.Background(), redisKey, encData, 30*time.Minute).Err()
	assert.NoError(t, err)

	// Request with 3 turns:
	// Turn 0: System
	// Turn 1: Historical tool response containing Ghost Stub
	// Turn 2: Latest User question
	promptReq := fmt.Sprintf(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "system", "content": "You are a financial analyst."},
			{"role": "tool", "content": "{\"_loopers_ref\":\"loopers:blob:%s\",\"tool\":\"query_db\",\"original_bytes\":4500}"},
			{"role": "user", "content": "Can you summarize the findings from the report?"}
		]
	}`, blobID)

	// Case A: Fast Tier model (isFrontierModel = false) -> MUST NOT HYDRATE
	unhydrated, err := HydratePromptTail(context.Background(), keyHash, proxyKey, sessionID, []byte(promptReq), rdb, false, 16000)
	assert.NoError(t, err)
	assert.Equal(t, []byte(promptReq), unhydrated)

	// Case B: Frontier model (isFrontierModel = true) -> APPEND-ONLY HYDRATION ON TAIL MESSAGE
	hydrated, err := HydratePromptTail(context.Background(), keyHash, proxyKey, sessionID, []byte(promptReq), rdb, true, 16000)
	assert.NoError(t, err)

	var hydratedMap map[string]interface{}
	err = json.Unmarshal(hydrated, &hydratedMap)
	assert.NoError(t, err)

	messages := hydratedMap["messages"].([]interface{})
	assert.Equal(t, 3, len(messages))

	// Invariant Check 1: Historical tool message (Turn 1) MUST BE 100% UNTOUCHED
	turn1 := messages[1].(map[string]interface{})
	turn1Content := turn1["content"].(string)
	assert.False(t, strings.Contains(turn1Content, "SECRET-INTERNAL-METRICS-REPORT"))
	assert.True(t, strings.Contains(turn1Content, "_loopers_ref"))

	// Invariant Check 2: Tail message (Turn 2) MUST have hydrated context appended
	turn2 := messages[2].(map[string]interface{})
	turn2Content := turn2["content"].(string)
	assert.True(t, strings.Contains(turn2Content, "Can you summarize the findings from the report?"))
	assert.True(t, strings.Contains(turn2Content, "<loopers_hydrated_context ref=\"loopers:blob:a1b2c3d4e5f60718\">"))
	assert.True(t, strings.Contains(turn2Content, "SECRET-INTERNAL-METRICS-REPORT: Q3 Revenue +42%"))
}
