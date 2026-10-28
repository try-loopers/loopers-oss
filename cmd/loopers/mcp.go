package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/try-loopers/loopers-oss/internal/budget"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/internal/mcp"
	proxyPkg "github.com/try-loopers/loopers-oss/internal/proxy"
)

var (
	mcpServerName  string
	mcpProxyURL    string
	mcpProxyKey    string
	mcpGhostThresh int64
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Model Context Protocol (MCP) stdio bridge, wrapper, and governance tools",
	Long: `Run and govern Model Context Protocol (MCP) servers with Loopers AI Firewall.
Provides:
  - 'loopers mcp bridge': Connects local agent stdio to Loopers HTTP MCP gateway.
  - 'loopers mcp wrap': Wraps a local child process MCP server, applying schema minification,
                        ghost tokens, and FinOps telemetry over stdin/stdout.`,
}

// mcpBridgeCmd connects an agent's stdin/stdout to Loopers HTTP MCP gateway.
var mcpBridgeCmd = &cobra.Command{
	Use:   "bridge",
	Short: "Bridge agent stdio (stdin/stdout) to Loopers HTTP MCP gateway",
	Long: `Allows agents using 'transport = "stdio"' (Claude Desktop, Cursor, ZeroClaw)
to communicate transparently with Loopers HTTP MCP endpoints. Every JSON-RPC request from stdin
is forwarded to http://localhost:8080/mcp/<server>/ and the minified/optimized response is printed to stdout.`,
	Run: func(cmd *cobra.Command, args []string) {
		if mcpServerName == "" {
			fmt.Fprintln(os.Stderr, "Error: --server flag is required (e.g. --server enterprise)")
			os.Exit(1)
		}

		proxyURL := mcpProxyURL
		if proxyURL == "" {
			proxyURL = os.Getenv("LOOPERS_PROXY_URL")
		}
		if proxyURL == "" {
			proxyURL = "http://127.0.0.1:8080"
		}
		proxyURL = strings.TrimSuffix(proxyURL, "/")

		key := mcpProxyKey
		if key == "" {
			key = os.Getenv("LOOPERS_PROXY_KEY")
		}

		endpoint := fmt.Sprintf("%s/mcp/%s/", proxyURL, mcpServerName)

		httpClient := &http.Client{
			Timeout: 120 * time.Second,
		}

		scanner := bufio.NewScanner(os.Stdin)
		// Allow up to 16MB lines for large MCP payloads
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 16*1024*1024)

		outWriter := bufio.NewWriter(os.Stdout)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}

			httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(line))
			if err != nil {
				fmt.Fprintf(os.Stderr, "loopers mcp bridge: failed to create request: %v\n", err)
				continue
			}

			httpReq.Header.Set("Content-Type", "application/json")
			if key != "" {
				httpReq.Header.Set("Authorization", "Bearer "+key)
			}

			resp, err := httpClient.Do(httpReq)
			if err != nil {
				fmt.Fprintf(os.Stderr, "loopers mcp bridge: failed to forward request: %v\n", err)
				// Return JSON-RPC error so agent doesn't hang
				var idVal interface{} = nil
				var rawMap map[string]interface{}
				if json.Unmarshal(line, &rawMap) == nil {
					idVal = rawMap["id"]
				}
				errResp := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      idVal,
					"error": map[string]interface{}{
						"code":    -32603,
						"message": fmt.Sprintf("Loopers MCP gateway error: %v", err),
					},
				}
				b, _ := json.Marshal(errResp)
				outWriter.Write(b)
				outWriter.WriteString("\n")
				outWriter.Flush()
				continue
			}

			respBody, err := io.ReadAll(resp.Body)
			resp.Body.Close()

			if err != nil {
				fmt.Fprintf(os.Stderr, "loopers mcp bridge: failed to read response: %v\n", err)
				continue
			}

			outWriter.Write(respBody)
			outWriter.WriteString("\n")
			outWriter.Flush()
		}

		if err := scanner.Err(); err != nil && err != io.EOF {
			fmt.Fprintf(os.Stderr, "loopers mcp bridge: scanner error: %v\n", err)
		}
	},
}

// mcpWrapCmd wraps a local child process MCP server, intercepting stdin/stdout to apply FinOps optimization.
var mcpWrapCmd = &cobra.Command{
	Use:   "wrap -- <command...>",
	Short: "Wrap a local stdio MCP child process, applying Schema Minification and Ghost Tokens",
	Long: `Wraps any local CLI MCP server (e.g. 'npx -y mcp-remote ...' or 'python -m mcp_server_fetch'),
intercepting standard I/O to minify tool schemas, create HKDF-encrypted Ghost Tokens for large payloads,
and record real-time FinOps savings to Loopers.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		serverName := mcpServerName
		if serverName == "" {
			serverName = "local"
		}

		proxyKey := mcpProxyKey
		if proxyKey == "" {
			proxyKey = os.Getenv("LOOPERS_PROXY_KEY")
		}

		var keyHash string
		if proxyKey != "" {
			keyHash = keyring.HashKey(proxyKey)
		}

		// Connect to Redis for FinOps telemetry and Ghost Token storage
		redisClient, _ := getRedisClient()
		if redisClient != nil {
			defer redisClient.Close()
			if keyHash == "" {
				// If no key passed, try finding first active key in Redis registry
				keys, err := fetchActiveKeys(redisClient)
				if err == nil && len(keys) > 0 {
					keyHash = keys[0].Value
				}
			}
		}

		sessionID := proxyPkg.ResolveGhostSessionID("", keyHash)
		threshold := mcpGhostThresh
		if threshold <= 0 {
			threshold = 4096
		}

		// Prepare child command
		targetCmd := exec.Command(args[0], args[1:]...)
		childStdin, err := targetCmd.StdinPipe()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to create stdin pipe for target MCP process")
		}
		childStdout, err := targetCmd.StdoutPipe()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to create stdout pipe for target MCP process")
		}
		targetCmd.Stderr = os.Stderr

		if err := targetCmd.Start(); err != nil {
			logging.Logger.Fatal().Err(err).Msgf("failed to start target MCP process: %s", args[0])
		}

		type pendingMeta struct {
			method   string
			toolName string
		}
		var pendingMu sync.Mutex
		pending := make(map[string]pendingMeta)

		// Goroutine 1: Read agent requests from os.Stdin, track methods, forward to childStdin
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			buf := make([]byte, 64*1024)
			scanner.Buffer(buf, 16*1024*1024)

			childWriter := bufio.NewWriter(childStdin)

			for scanner.Scan() {
				line := scanner.Bytes()
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}

				var req struct {
					ID     interface{}            `json:"id"`
					Method string                 `json:"method"`
					Params map[string]interface{} `json:"params"`
				}

				if err := json.Unmarshal(line, &req); err == nil && req.ID != nil {
					idStr := fmt.Sprintf("%v", req.ID)
					var toolName string
					if req.Method == "tools/call" && req.Params != nil {
						if tn, ok := req.Params["name"].(string); ok {
							toolName = tn
						}
					}
					pendingMu.Lock()
					pending[idStr] = pendingMeta{method: req.Method, toolName: toolName}
					pendingMu.Unlock()
				}

				childWriter.Write(line)
				childWriter.WriteString("\n")
				childWriter.Flush()
			}
			childStdin.Close()
		}()

		// Main routine: Read childStdout, apply Schema Minification / Ghost Tokens, write to os.Stdout
		stdoutScanner := bufio.NewScanner(childStdout)
		buf := make([]byte, 64*1024)
		stdoutScanner.Buffer(buf, 16*1024*1024)
		agentWriter := bufio.NewWriter(os.Stdout)

		sanitizerCfg := mcp.SanitizerConfig{
			MinifySchemas:        true,
			MaxDescriptionLength: 512,
		}

		for stdoutScanner.Scan() {
			line := stdoutScanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}

			var resp struct {
				ID     interface{}     `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  interface{}     `json:"error"`
			}

			outputLine := line

			if err := json.Unmarshal(line, &resp); err == nil && resp.ID != nil && resp.Error == nil {
				idStr := fmt.Sprintf("%v", resp.ID)
				pendingMu.Lock()
				meta, hasMeta := pending[idStr]
				delete(pending, idStr)
				pendingMu.Unlock()

				if hasMeta {
					// 1. Schema Minification for tools/list
					if meta.method == "tools/list" {
						sanitized, sErr := mcp.SanitizeToolList(line, sanitizerCfg, serverName)
						if sErr == nil && len(sanitized) > 0 {
							savedBytes := int64(len(line) - len(sanitized))
							if savedBytes > 0 && redisClient != nil && keyHash != "" {
								savedTokens := savedBytes / 4
								ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
								_ = redisClient.RecordFinOpsTelemetry(ctx, keyHash, sessionID, budget.FinOpsTelemetryDelta{
									SchemaTokensSaved: savedTokens,
								})
								cancel()
							}
							outputLine = sanitized
						}
					}

					// 2. Ghost Tokens for tools/call
					if meta.method == "tools/call" && int64(len(line)) > threshold && redisClient != nil && keyHash != "" {
						var rpcMap map[string]interface{}
						if json.Unmarshal(line, &rpcMap) == nil {
							if resVal, hasRes := rpcMap["result"]; hasRes && resVal != nil {
								var rawPayload []byte
								var isContentText bool
								var contentList []interface{}
								var firstItem map[string]interface{}

								if resMap, ok := resVal.(map[string]interface{}); ok {
									if cList, ok := resMap["content"].([]interface{}); ok && len(cList) > 0 {
										if itemMap, ok := cList[0].(map[string]interface{}); ok {
											if text, ok := itemMap["text"].(string); ok && int64(len(text)) > threshold {
												rawPayload = []byte(text)
												isContentText = true
												contentList = cList
												firstItem = itemMap
											}
										}
									}
								}

								if len(rawPayload) == 0 && int64(len(line)) > threshold {
									rawPayload, _ = json.Marshal(resVal)
								}

								if len(rawPayload) > 0 {
									blobID := proxyPkg.ContentBlobID(rawPayload)
									ghostKey := proxyPkg.DeriveGhostKey(proxyKey, sessionID)
									enc, encErr := proxyPkg.EncryptGhostBlob(ghostKey, rawPayload)
									if encErr == nil {
										rdbKey := fmt.Sprintf("loopers:blob:{%s}:%s:%s", keyHash, sessionID, blobID)
										ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
										if setErr := redisClient.GetUnderlyingClient().Set(ctx, rdbKey, enc, 1800*time.Second).Err(); setErr == nil {
											stubBytes, stubErr := proxyPkg.CreateGhostStub(meta.toolName, rawPayload, blobID)
											if stubErr == nil {
												if isContentText {
													firstItem["text"] = string(stubBytes)
													contentList[0] = firstItem
													resMap := resVal.(map[string]interface{})
													resMap["content"] = contentList
													rpcMap["result"] = resMap
												} else {
													var stubObj interface{}
													_ = json.Unmarshal(stubBytes, &stubObj)
													rpcMap["result"] = stubObj
												}
												if newBody, mErr := json.Marshal(rpcMap); mErr == nil {
													outputLine = newBody
													savedBytes := int64(len(rawPayload) - len(stubBytes))
													if savedBytes > 0 {
														savedTokens := savedBytes / 4
														_ = redisClient.RecordFinOpsTelemetry(ctx, keyHash, sessionID, budget.FinOpsTelemetryDelta{
															GhostTokensSaved:  savedTokens,
															GhostPayloadBytes: savedBytes,
															GhostStubsCreated: 1,
														})
													}
												}
											}
										}
										cancel()
									}
								}
							}
						}
					}
				}
			}

			agentWriter.Write(outputLine)
			agentWriter.WriteString("\n")
			agentWriter.Flush()
		}

		if err := targetCmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			os.Exit(1)
		}
	},
}

func init() {
	mcpBridgeCmd.Flags().StringVarP(&mcpServerName, "server", "s", "", "Name of the MCP server configured in Loopers (required)")
	mcpBridgeCmd.Flags().StringVarP(&mcpProxyURL, "url", "u", "http://127.0.0.1:8080", "Base URL of the Loopers AI Firewall proxy")
	mcpBridgeCmd.Flags().StringVarP(&mcpProxyKey, "key", "k", "", "Loopers agent key (defaults to LOOPERS_PROXY_KEY env)")

	mcpWrapCmd.Flags().StringVarP(&mcpServerName, "server", "s", "local", "Logical name of the MCP server for metrics")
	mcpWrapCmd.Flags().StringVarP(&mcpProxyKey, "key", "k", "", "Loopers agent key (defaults to LOOPERS_PROXY_KEY env)")
	mcpWrapCmd.Flags().Int64VarP(&mcpGhostThresh, "threshold", "t", 4096, "Payload size threshold in bytes to trigger Ghost Token offloading")

	mcpCmd.AddCommand(mcpBridgeCmd)
	mcpCmd.AddCommand(mcpWrapCmd)

	rootCmd.AddCommand(mcpCmd)
}
