// Package wire holds what the HTTP adapters share: posting with retries,
// reading server-sent events, error text, and the failed message.
package wire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spike/gobot/agent"
)

// Retry says how a request that fails before it streams is retried: on a
// connection error, 408, 409, 429, or 5xx, up to Max times, waiting
// retry-after or Backoff doubling to 8 s. A wait above MaxDelay fails instead.
type Retry struct {
	Max      int
	Backoff  time.Duration
	MaxDelay time.Duration
}

// DefaultRetry is what every adapter starts with.
var DefaultRetry = Retry{Max: 2, Backoff: 500 * time.Millisecond, MaxDelay: 60 * time.Second}

// Post sends body to url with the headers and returns a 2xx response.
func Post(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte, r Retry) (*http.Response, error) {
	delay := r.Backoff
	for attempt := 0; ; attempt++ {
		hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		hreq.Header.Set("content-type", "application/json")
		hreq.Header.Set("accept", "text/event-stream")
		for k, v := range headers {
			hreq.Header.Set(k, v)
		}

		resp, err := client.Do(hreq)
		wait := delay
		switch {
		case err != nil:
			if ctx.Err() != nil || attempt >= r.Max {
				return nil, err
			}
		case resp.StatusCode/100 == 2:
			return resp, nil
		default:
			msg := apiError(resp)
			retryable := resp.StatusCode == 408 || resp.StatusCode == 409 || resp.StatusCode == 429 || resp.StatusCode >= 500
			if ra := retryAfter(resp); ra > 0 {
				wait = ra
			}
			resp.Body.Close()
			if !retryable || attempt >= r.Max {
				return nil, fmt.Errorf("%d: %s", resp.StatusCode, msg)
			}
			if wait > r.MaxDelay {
				return nil, fmt.Errorf("%d: %s (retry after %s)", resp.StatusCode, msg, wait)
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		if delay *= 2; delay > 8*time.Second {
			delay = 8 * time.Second
		}
	}
}

// apiError is the server's error.message, else the body, else the status.
func apiError(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	if s := strings.TrimSpace(string(b)); s != "" {
		return s
	}
	return resp.Status
}

func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("retry-after")
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

// ReadSSE calls handle with each event's name and data until handle returns
// done, an error, or the stream ends.
func ReadSSE(r io.Reader, handle func(event, data string) (done bool, err error)) error {
	br := bufio.NewReaderSize(r, 1<<16)
	var event string
	for {
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			done, herr := handle(event, strings.TrimSpace(line[len("data:"):]))
			if herr != nil {
				return herr
			}
			if done {
				return nil
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// Fail ends a call: StopAborted when ctx was cancelled, else StopError with
// err's text as an error part.
func Fail(ctx context.Context, partial agent.Message, providerID, model string, err error) agent.Message {
	partial.Role = agent.RoleAssistant
	partial.Provider, partial.Model = providerID, model
	if ctx.Err() != nil {
		partial.StopReason = agent.StopAborted
		return partial
	}
	partial.StopReason = agent.StopError
	partial.Parts = append(partial.Parts, agent.Part{Kind: agent.PartText, Text: err.Error(), IsError: true})
	return partial
}

// Args turns streamed tool arguments into the JSON a Part holds: "" is {},
// and text that is not JSON becomes a JSON string, which argument validation
// then rejects so the model reads the error.
func Args(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" {
		return json.RawMessage(`{}`)
	}
	if !json.Valid([]byte(s)) {
		b, _ := json.Marshal(s)
		return b
	}
	return json.RawMessage(s)
}

var idUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// CallID gives a tool call id the [a-zA-Z0-9_-]{1,64} shape every API takes.
func CallID(id string) string {
	id = idUnsafe.ReplaceAllString(id, "_")
	if len(id) > 64 {
		id = id[:64]
	}
	if id == "" {
		id = "call"
	}
	return id
}
