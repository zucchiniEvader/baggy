package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/zucchiniEvader/baggy/team"
)

// Client is a desktop's end of a Server on another machine.
type Client struct {
	Base  string // http://host:port
	Token string
	Name  string // the server's, from the link
	HTTP  *http.Client
}

// ParseLink reads a baggy://pair?host=…&port=…&token=… link (PairURL).
func ParseLink(link string) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "baggy" {
		return nil, errors.New("not a baggy:// link")
	}
	q := u.Query()
	host, token := q.Get("host"), q.Get("token")
	port, _ := strconv.Atoi(q.Get("port"))
	if host == "" || token == "" {
		return nil, errors.New("the link has no host or token")
	}
	if port == 0 {
		port = DefaultPort
	}
	base := "http://" + host + ":" + strconv.Itoa(port)
	if strings.Contains(host, ":") { // IPv6
		base = "http://[" + host + "]:" + strconv.Itoa(port)
	}
	return &Client{Base: base, Token: token, Name: q.Get("name")}, nil
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		var e struct{ Error string }
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("%s: %s", res.Status, e.Error)
		}
		return nil, errors.New(res.Status)
	}
	return b, nil
}

// Hello is what the server says of itself.
type Hello struct {
	App, Name, Version string
}

// Hello asks the server who it is, which also checks the token.
func (c *Client) Hello(ctx context.Context) (Hello, error) {
	var h Hello
	b, err := c.do(ctx, "GET", "/v1/hello", nil)
	if err == nil {
		err = json.Unmarshal(b, &h)
	}
	return h, err
}

// ErrUnreachable wraps a call that never reached the server.
var ErrUnreachable = errors.New("the server cannot be reached")

// Call runs method ("Store.Bots", "Host.Snapshot") with args, decoding its
// results into out, which point at them in order. The method's error comes
// back as an error with its message.
func (c *Client) Call(method string, args []any, out ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ret, err := c.CallRaw(ctx, method, args)
	if err != nil {
		return err
	}
	return ret.Decode(method, out...)
}

// CallRaw is Call leaving the results as JSON.
func (c *Client) CallRaw(ctx context.Context, method string, args []any) (Return, error) {
	call := Call{Method: method, Args: make([]json.RawMessage, len(args))}
	for i, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			return Return{}, err
		}
		call.Args[i] = b
	}
	body, _ := json.Marshal(call)
	b, err := c.do(ctx, "POST", "/v1/call", body)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) || ctx.Err() != nil {
			return Return{}, fmt.Errorf("%w: %v", ErrUnreachable, ue.Err)
		}
		return Return{}, err
	}
	var ret Return
	err = json.Unmarshal(b, &ret)
	return ret, err
}

// Decode puts r's results into out, in order, and gives the method's error.
func (r Return) Decode(method string, out ...any) error {
	for i, o := range out {
		if i < len(r.Results) && o != nil {
			if err := json.Unmarshal(r.Results[i], o); err != nil {
				return fmt.Errorf("%s: %w", method, err)
			}
		}
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

// Download reads one of the server's files: path is /v1/avatars/<name> or
// /v1/files/<id>/<name>.
func (c *Client) Download(path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return c.do(ctx, "GET", path, nil)
}

// Events follows the server's events in full until ctx ends, connecting
// again with backoff up to 15 seconds. connected tells each time the
// connection is made (nil) or lost (why); after it is made again, events
// in between are lost, and the caller reads its state again.
func (c *Client) Events(ctx context.Context, on func(team.Event), connected func(error)) {
	wait := time.Second
	for ctx.Err() == nil {
		err := c.follow(ctx, on, func() { wait = time.Second; connected(nil) })
		if ctx.Err() != nil {
			return
		}
		connected(err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 15*time.Second)
	}
}

func (c *Client) follow(ctx context.Context, on func(team.Event), up func()) error {
	u := strings.Replace(c.Base, "http", "ws", 1) + "/v1/events?full=1"
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.HTTP, HTTPHeader: http.Header{"Authorization": {"Bearer " + c.Token}}})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 20)
	// A peer gone from the tailnet closes nothing: a ping that goes
	// unanswered ends the connection.
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pctx)
				cancel()
				if err != nil {
					conn.CloseNow()
					return
				}
			}
		}
	}()
	up()
	for {
		_, b, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var fe FullEvent
		if json.Unmarshal(b, &fe) == nil {
			on(fe.Event())
		}
	}
}
