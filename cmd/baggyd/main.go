// Command baggyd runs Baggy's bots with no window, for Baggy desktops on
// other machines of the same tailnet (docs/remote.md). It listens on this
// machine's Tailscale address and prints the link a desktop pastes in
// Settings › General › Runtime; phones pair with the same link.
//
//	baggyd [-data dir] [-port 7841] [-listen addr] [-reset-token]
//	baggyd link [-data dir] [-port 7841]   # prints the links again
//	baggyd version
//
// The provider, when none is stored, comes from the environment as the
// desktop's does (connect.FromEnv).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/zucchiniEvader/baggy/host"
	"github.com/zucchiniEvader/baggy/provider/connect"
	"github.com/zucchiniEvader/baggy/remote"
	"github.com/zucchiniEvader/baggy/store"
)

// version is set at build time.
var version = "dev"

func defaultData() string {
	if d := os.Getenv("GOBOT_DATA"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Baggy")
}

func defaultWorkspaces() string {
	if d := os.Getenv("GOBOT_WORKSPACES"); d != "" {
		return d
	}
	if os.Getenv("GOBOT_DATA") != "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".baggy", "workspaces")
}

func main() {
	cmd := ""
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd, os.Args = os.Args[1], append(os.Args[:1], os.Args[2:]...)
	}
	data := flag.String("data", defaultData(), "the folder of the database, attachments and mcp.json")
	workspaces := flag.String("workspaces", defaultWorkspaces(), "the bots' workspaces (empty: beside the database)")
	port := flag.Int("port", remote.DefaultPort, "the port to listen on")
	listen := flag.String("listen", "", "the address to listen on (default: this machine's Tailscale address)")
	reset := flag.Bool("reset-token", false, "make a new token: desktops and phones connected before must take the new link")
	flag.Parse()
	switch cmd {
	case "":
	case "version":
		fmt.Println(version)
		return
	case "link":
		printLinks(*data, *port)
		return
	default:
		log.Fatalf("baggyd: no command %q (link, version)", cmd)
	}

	addr := *listen
	var hosts []string
	if addr == "" {
		hosts = remote.TailnetAddresses()
		if len(hosts) == 0 {
			log.Fatal("this machine has no Tailscale address: start Tailscale, or pass -listen")
		}
		addr = net.JoinHostPort(hosts[0], strconv.Itoa(*port))
	} else if h, _, err := net.SplitHostPort(addr); err == nil && h != "" && h != "0.0.0.0" && h != "::" {
		hosts = []string{h}
	} else {
		hosts = remote.TailnetAddresses()
	}

	srv := &remote.Server{Name: hostname(), Version: version}
	h, err := host.Open(host.Options{Data: *data, Workspaces: *workspaces, Fallback: connect.FromEnv("claude-sonnet-5-5"), Emit: srv.Publish})
	if err != nil {
		log.Fatal(err)
	}
	srv.Host = h
	srv.Token = h.Store.Setting("phone.token")
	if srv.Token == "" || *reset {
		srv.Token = remote.NewToken()
		h.Store.SetSetting("phone.token", srv.Token)
	}
	if err := srv.Start(addr); err != nil {
		h.Close()
		log.Fatal(err)
	}
	h.Team.StartRoutines(context.Background())
	_, p, _ := net.SplitHostPort(addr)
	pn, _ := strconv.Atoi(p)
	fmt.Printf("Baggy %s on %s, data in %s\n", version, addr, *data)
	fmt.Println("Paste a link in Baggy's Settings › General › Runtime on another machine of the tailnet:")
	for _, a := range hosts {
		fmt.Println("  " + remote.PairURL(a, pn, srv.Token, srv.Name))
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	srv.Stop()
	h.Close()
}

func hostname() string {
	name, _ := os.Hostname()
	return strings.TrimSuffix(name, ".local")
}

// printLinks prints the links of the token kept in data, for a baggyd
// that runs as a service and printed them to its log.
func printLinks(data string, port int) {
	st, err := store.Open(data)
	if err != nil {
		log.Fatal(err)
	}
	token := st.Setting("phone.token")
	st.Close()
	if token == "" {
		log.Fatal("no token yet: start baggyd once")
	}
	hosts := remote.TailnetAddresses()
	if len(hosts) == 0 {
		log.Fatal("this machine has no Tailscale address")
	}
	for _, a := range hosts {
		fmt.Println(remote.PairURL(a, port, token, hostname()))
	}
}
