package connect

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models": // OpenAI's shape, an OpenRouter-like entry, and things chat cannot use
			if r.Header.Get("authorization") != "Bearer k" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"data":[
				{"id":"gpt-5.5"},
				{"id":"text-embedding-3-small"},
				{"id":"whisper-1"},
				{"id":"vendor/vision-1","name":"Vision One","context_length":200000,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
				{"id":"vendor/painter","architecture":{"output_modalities":["image"]}},
				{"id":"local-llm","max_model_len":32768,"capabilities":{"vision":false}}]}`))
		case "/deep/models":
			w.Write([]byte(`{"data":[{"id":"deepseek-flash"}]}`))
		case "/anthropic/v1/models":
			if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") == "" || r.URL.Query().Get("limit") != "1000" {
				w.WriteHeader(403)
				return
			}
			w.Write([]byte(`{"data":[{"id":"claude-x","display_name":"Claude X","type":"model"}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	got, err := ListModels(ctx, Chat, srv.URL+"/v1/", "k")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if len(got) != 3 || got[0].ID != "gpt-5.5" || got[1].Name != "Vision One" || got[1].Window != 200000 || !got[1].Images ||
		got[2].ID != "local-llm" || got[2].Window != 32768 || got[2].Images {
		t.Fatalf("listed %q: %+v", ids, got)
	}
	if _, err := ListModels(ctx, Responses, srv.URL+"/v1", "wrong"); !errors.Is(err, ErrKey) {
		t.Fatalf("bad key: %v", err)
	}
	if got, err := ListModels(ctx, Anthropic, srv.URL+"/anthropic", "k"); err != nil || len(got) != 1 || got[0].Name != "Claude X" {
		t.Fatalf("messages: %+v %v", got, err)
	}
	if got, err := ListModels(ctx, Anthropic, srv.URL+"/deep/anthropic", "k"); err != nil || len(got) != 1 || got[0].ID != "deepseek-flash" {
		t.Fatalf("root beside /anthropic: %+v %v", got, err)
	}
	if _, err := ListModels(ctx, Chat, srv.URL+"/nothing", "k"); !errors.Is(err, ErrNoList) {
		t.Fatalf("no list: %v", err)
	}
}

func TestParseModelsBareArrayAndOllama(t *testing.T) {
	got, err := parseModels([]byte(`[{"id":"a"},{"id":"a"}]`))
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = parseModels([]byte(`{"models":[{"name":"qwen3:8b"},{"name":"nomic-embed-text"}]}`))
	if err != nil || len(got) != 1 || got[0].ID != "qwen3:8b" || got[0].Name != "" {
		t.Fatalf("%+v %v", got, err)
	}
}
