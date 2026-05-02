// SPDX-License-Identifier: MIT

// bedrock-probe is a small CLI wrapper around the synthetic RakNet probe.
// CI uses it for post-deploy smoke tests; you can also run it locally to
// quickly check that a Bedrock server is alive and what it claims:
//
//	go run ./cmd/bedrock-probe 127.0.0.1:19132
//	go run ./cmd/bedrock-probe -wait=30s -addr 127.0.0.1:19132
//
// Exit codes:
//
//	0  pong received, MOTD parsed
//	1  no response, parse error, or timeout
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"go.minekube.com/geyserlite/internal/synthetic"
)

func main() {
	var (
		addr   = flag.String("addr", "", "host:port of the Bedrock server (positional arg also accepted)")
		wait   = flag.Duration("wait", 0, "if >0, retry probe until success or timeout (e.g. 30s)")
		jsonOn = flag.Bool("json", false, "emit JSON instead of human-readable lines")
		quiet  = flag.Bool("quiet", false, "exit silently on success; only print on failure")
	)
	flag.Parse()
	if *addr == "" && flag.NArg() > 0 {
		*addr = flag.Arg(0)
	}
	if *addr == "" {
		fmt.Fprintln(os.Stderr, "bedrock-probe: missing -addr")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), maxDuration(3*time.Second, *wait+time.Second))
	defer cancel()

	var (
		motd *synthetic.MOTD
		err  error
	)
	if *wait > 0 {
		motd, err = synthetic.Wait(ctx, *addr, 250*time.Millisecond)
	} else {
		motd, err = synthetic.Probe(ctx, *addr)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bedrock-probe: %v\n", err)
		os.Exit(1)
	}
	if *quiet {
		return
	}
	if *jsonOn {
		_ = json.NewEncoder(os.Stdout).Encode(motd)
		return
	}
	fmt.Printf("addr        %s\n", *addr)
	fmt.Printf("edition     %s\n", motd.Edition)
	fmt.Printf("motd        %s\n", motd.Line1)
	fmt.Printf("sub         %s\n", motd.Line2)
	fmt.Printf("version     %s (protocol %d)\n", motd.GameVersion, motd.ProtocolVer)
	fmt.Printf("players     %d / %d\n", motd.Players, motd.MaxPlayers)
	fmt.Printf("rtt         %.2f ms\n", motd.RTTMillis)
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
