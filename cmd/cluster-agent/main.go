package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/s1ckdark/hydra/internal/agent"
	"github.com/s1ckdark/hydra/internal/domain"
	"github.com/s1ckdark/hydra/internal/infra/ai/claude"
	"github.com/s1ckdark/hydra/internal/infra/ai/lmstudio"
	"github.com/s1ckdark/hydra/internal/infra/ai/ollama"
)

func main() {
	nodeID := flag.String("node-id", "", "Node ID (required)")
	orchID := flag.String("orch-id", "", "Orch ID (required)")
	role := flag.String("role", "worker", "Node role: head or worker")
	port := flag.Int("port", 9090, "HTTP listen port")
	heartbeat := flag.Duration("heartbeat", 3*time.Second, "Heartbeat interval")
	timeout := flag.Duration("timeout", 15*time.Second, "Failure timeout")
	anthropicKey := flag.String("anthropic-key", "", "Anthropic API key (default: $ANTHROPIC_API_KEY)")
	aiProvider := flag.String("ai-provider", "", "AI provider: claude, ollama, lmstudio (default: auto-detect)")
	ollamaEndpoint := flag.String("ollama-endpoint", "", "Ollama endpoint (default: http://localhost:11434)")
	ollamaModel := flag.String("ollama-model", "", "Ollama model name (default: gpt-oss:20b)")
	lmstudioEndpoint := flag.String("lmstudio-endpoint", "", "LM Studio endpoint (default: http://localhost:1234)")
	lmstudioModel := flag.String("lmstudio-model", "", "LM Studio model name (default: gpt-oss-20b)")

	flag.Parse()

	// Resolve from env if not set via flag. HYDRA_ is the canonical prefix;
	// NAGA_ is honored as a legacy alias so deployments scripted before the
	// rename continue to work.
	if *anthropicKey == "" {
		*anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if *aiProvider == "" {
		*aiProvider = envFallback("HYDRA_AI_PROVIDER", "NAGA_AI_PROVIDER")
	}
	if *ollamaEndpoint == "" {
		*ollamaEndpoint = envFallback("HYDRA_OLLAMA_ENDPOINT", "NAGA_OLLAMA_ENDPOINT")
	}
	if *ollamaModel == "" {
		*ollamaModel = envFallback("HYDRA_OLLAMA_MODEL", "NAGA_OLLAMA_MODEL")
	}
	if *lmstudioEndpoint == "" {
		*lmstudioEndpoint = envFallback("HYDRA_LMSTUDIO_ENDPOINT", "NAGA_LMSTUDIO_ENDPOINT")
	}
	if *lmstudioModel == "" {
		*lmstudioModel = envFallback("HYDRA_LMSTUDIO_MODEL", "NAGA_LMSTUDIO_MODEL")
	}

	if *nodeID == "" {
		fmt.Fprintln(os.Stderr, "error: --node-id is required")
		flag.Usage()
		os.Exit(1)
	}
	if *orchID == "" {
		fmt.Fprintln(os.Stderr, "error: --orch-id is required")
		flag.Usage()
		os.Exit(1)
	}

	var nodeRole domain.NodeRole
	switch *role {
	case "coordinator":
		nodeRole = domain.NodeRoleHead
	case "worker":
		nodeRole = domain.NodeRoleWorker
	default:
		fmt.Fprintf(os.Stderr, "error: invalid role %q, must be head or worker\n", *role)
		os.Exit(1)
	}

	cfg := agent.AgentConfig{
		NodeID:            *nodeID,
		OrchID:         *orchID,
		Role:              nodeRole,
		ListenAddr:        fmt.Sprintf(":%d", *port),
		HeartbeatInterval: *heartbeat,
		FailureTimeout:    *timeout,
		HeadSelector:      resolveHeadSelector(*aiProvider, *anthropicKey, *ollamaEndpoint, *ollamaModel, *lmstudioEndpoint, *lmstudioModel),
	}

	a := agent.NewAgent(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down", sig)
		cancel()
	}()

	if err := a.Run(ctx); err != nil {
		log.Fatalf("agent error: %v", err)
	}
}

func resolveHeadSelector(provider, anthropicKey, ollamaEndpoint, ollamaModel, lmstudioEndpoint, lmstudioModel string) agent.HeadSelector {
	switch provider {
	case "ollama":
		log.Printf("using ollama provider (endpoint=%s, model=%s)", ollamaEndpoint, ollamaModel)
		return ollama.NewProvider(ollamaEndpoint, ollamaModel)
	case "lmstudio":
		log.Printf("using lmstudio provider (endpoint=%s, model=%s)", lmstudioEndpoint, lmstudioModel)
		return lmstudio.NewProvider(lmstudioEndpoint, lmstudioModel)
	case "claude":
		if anthropicKey == "" {
			return nil
		}
		return claude.NewProvider(anthropicKey, "")
	default:
		// Auto-detect: claude → ollama → lmstudio → nil
		if anthropicKey != "" {
			return claude.NewProvider(anthropicKey, "")
		}
		if ollamaEndpoint != "" || ollamaModel != "" {
			return ollama.NewProvider(ollamaEndpoint, ollamaModel)
		}
		if lmstudioEndpoint != "" || lmstudioModel != "" {
			return lmstudio.NewProvider(lmstudioEndpoint, lmstudioModel)
		}
		// Probe default local endpoints
		if probeEndpoint("http://localhost:11434/api/tags") {
			log.Println("auto-detected ollama on localhost:11434")
			return ollama.NewProvider("", "")
		}
		if probeEndpoint("http://localhost:1234/v1/models") {
			log.Println("auto-detected lmstudio on localhost:1234")
			return lmstudio.NewProvider("", "")
		}
		log.Println("no AI provider configured, using rule-based fallback")
		return nil
	}
}

func probeEndpoint(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// envFallback returns the first non-empty value across the supplied env var
// names. Used to honor a legacy prefix while preferring the canonical one.
func envFallback(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}
