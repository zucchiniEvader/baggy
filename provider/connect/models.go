package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/zucchiniEvader/baggy/provider/catalog"
)

// Listed is a model a server lists.
type Listed struct {
	ID     string
	Name   string // "" when the list gives none
	Window int    // context window in tokens; 0 when unknown
	Images bool
}

var (
	// ErrKey is a server refusing the key, or asking for one.
	ErrKey = errors.New("the server refused the API key")
	// ErrNoList is a server with no model list; its models are typed in.
	ErrNoList = errors.New("the server does not list its models")
)

// ListModels asks a server for the models chat can use, in its order, 20
// seconds at most: GET {base}/models for Chat Completions and Responses,
// {base}/v1/models for the Messages API. A Messages endpoint at
// …/anthropic with no list of its own (DeepSeek's, Moonshot's) is asked
// for the list at the root beside it, OpenAI's way. Embedding, speech,
// image, and moderation models are left out.
func ListModels(ctx context.Context, api, baseURL, apiKey string) ([]Listed, error) {
	ms, err := listModels(ctx, api, baseURL, apiKey)
	root, cut := strings.CutSuffix(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "/anthropic")
	if errors.Is(err, ErrNoList) && (api == Anthropic || api == "") && cut {
		if ms, rerr := listModels(ctx, Chat, root, apiKey); rerr == nil {
			return ms, nil
		}
	}
	return ms, err
}

func listModels(ctx context.Context, api, baseURL, apiKey string) ([]Listed, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	var url string
	headers := map[string]string{}
	switch api {
	case Anthropic, "":
		if base == "" {
			base = "https://api.anthropic.com"
		}
		url = base + "/v1/models?limit=1000"
		if strings.HasSuffix(base, "/v1") {
			url = base + "/models?limit=1000"
		}
		headers["anthropic-version"] = "2023-06-01"
		if apiKey != "" {
			headers["x-api-key"] = apiKey
		}
	case Chat, Responses:
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		url = base + "/models"
		if apiKey != "" {
			headers["authorization"] = "Bearer " + apiKey
		}
	default:
		return nil, fmt.Errorf("unknown API %q", api)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrKey
	case resp.StatusCode/100 != 2:
		return nil, ErrNoList
	}
	return parseModels(body)
}

type listedModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	Type         string `json:"type"`
	Architecture struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Capabilities struct {
		Vision         *bool `json:"vision"`
		CompletionChat *bool `json:"completion_chat"`
	} `json:"capabilities"`
	ContextLength    int `json:"context_length"`
	ContextWindow    int `json:"context_window"`
	MaxModelLen      int `json:"max_model_len"`
	MaxContextLength int `json:"max_context_length"`
	MaxInputTokens   int `json:"max_input_tokens"`
}

// parseModels reads a list as `data`, `models`, or a bare array.
func parseModels(body []byte) ([]Listed, error) {
	var list []listedModel
	var wrapped struct {
		Data   []listedModel `json:"data"`
		Models []listedModel `json:"models"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && (wrapped.Data != nil || wrapped.Models != nil) {
		list = append(wrapped.Data, wrapped.Models...)
	} else if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("the model list is not JSON: %w", err)
	}
	var out []Listed
	seen := map[string]bool{}
	for _, m := range list {
		id := m.ID
		if id == "" {
			id = m.Name // Ollama's own list names them
		}
		if id == "" || seen[id] || !chatModel(id, m) {
			continue
		}
		seen[id] = true
		l := Listed{ID: id, Name: m.DisplayName, Window: max(m.ContextLength, m.ContextWindow, m.MaxModelLen, m.MaxContextLength, m.MaxInputTokens)}
		if l.Name == "" && m.Name != id {
			l.Name = m.Name
		}
		known := false
		for _, in := range append(m.Architecture.InputModalities, m.Modalities.Input...) {
			known = true
			l.Images = l.Images || in == "image"
		}
		if v := m.Capabilities.Vision; v != nil {
			known, l.Images = true, *v
		}
		if !known {
			l.Images = catalog.Images(id)
		}
		if c, ok := catalog.Find(id); ok && l.Window == 0 {
			l.Window = c.ContextWindow
		}
		out = append(out, l)
	}
	return out, nil
}

// chatModel tells a model chat can use from embeddings, rerankers, speech,
// realtime audio, image and video generation, moderation, and base
// completion models: by its type, its output, or words in its id.
func chatModel(id string, m listedModel) bool {
	switch strings.ToLower(m.Type) {
	case "embedding", "embeddings", "rerank", "reranker", "image", "images", "audio", "tts", "stt", "moderation", "video", "decisions", "evaluation":
		return false
	}
	if v := m.Capabilities.CompletionChat; v != nil && !*v {
		return false
	}
	if outs := append(m.Architecture.OutputModalities, m.Modalities.Output...); len(outs) > 0 && !slices.Contains(outs, "text") {
		return false
	}
	lower := strings.ToLower(id)
	for _, w := range []string{"embed", "rerank", "tts", "whisper", "transcribe", "speech", "realtime", "dall-e", "gpt-image", "image-gen", "imagen", "moderation", "sora", "veo", "babbage", "davinci"} {
		if strings.Contains(lower, w) {
			return false
		}
	}
	return true
}
