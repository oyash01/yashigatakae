package mempalace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Embedder produces a fixed-dimension vector for a given piece of text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Model() string // e.g. "voyage-3-lite", "text-embedding-3-small"
}

// AutoEmbedder picks Voyage if VOYAGE_API_KEY is set, else OpenAI if
// OPENAI_API_KEY is set. Returns a NoEmbedder if neither — in that case
// recall falls back to keyword search.
func AutoEmbedder() Embedder {
	if k := strings.TrimSpace(os.Getenv("VOYAGE_API_KEY")); k != "" {
		return &voyage{key: k, model: "voyage-3-lite"}
	}
	if k := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); k != "" {
		return &openAI{key: k, model: "text-embedding-3-small"}
	}
	return noEmbedder{}
}

// noEmbedder is the fallback when no API key is configured.
type noEmbedder struct{}

func (noEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("no embedding API key configured (set VOYAGE_API_KEY or OPENAI_API_KEY in ~/.yashigatakae/secrets.env)")
}
func (noEmbedder) Model() string { return "none" }

// voyage calls https://api.voyageai.com/v1/embeddings.
type voyage struct {
	key   string
	model string
}

func (v *voyage) Model() string { return v.model }
func (v *voyage) Embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"input":      []string{text},
		"model":      v.model,
		"input_type": "document",
	}
	return doEmbed(ctx, "https://api.voyageai.com/v1/embeddings", v.key, body, "voyage")
}

// openAI calls https://api.openai.com/v1/embeddings.
type openAI struct {
	key   string
	model string
}

func (o *openAI) Model() string { return o.model }
func (o *openAI) Embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{
		"input": text,
		"model": o.model,
	}
	return doEmbed(ctx, "https://api.openai.com/v1/embeddings", o.key, body, "openai")
}

// doEmbed is a tiny shared HTTP helper. Both Voyage and OpenAI return
// {"data":[{"embedding":[...floats...]}]} on success.
func doEmbed(ctx context.Context, url, apiKey string, body map[string]any, label string) ([]float32, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: HTTP %d: %s", label, resp.StatusCode, string(respBody))
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("%s: parse: %w", label, err)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("%s: empty embedding", label)
	}
	return parsed.Data[0].Embedding, nil
}
