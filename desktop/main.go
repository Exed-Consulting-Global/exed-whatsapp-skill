// Command exed-whatsapp is a single self-contained stdio MCP server that
// embeds a WhatsApp client (whatsmeow). It is launched by the Claude Desktop
// app; stdin/stdout carry the MCP JSON-RPC transport, so all logging goes to
// stderr. There is no REST server and no separate process.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Exed-Consulting-Global/exed-whatsapp-skill/desktop/internal/mcpserver"
	"github.com/Exed-Consulting-Global/exed-whatsapp-skill/desktop/internal/wa"
)

func main() {
	// Root context cancelled on SIGINT/SIGTERM so we disconnect cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Build the WhatsApp client (opens absolute-path DBs, registers handlers).
	client, err := wa.New(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: failed to initialize WhatsApp client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Connect if already paired; otherwise stay offline until conectar_whatsapp.
	// Never blocks on pairing (no terminal is available under Claude Desktop).
	if err := client.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: initial connect failed: %v\n", err)
	}

	// Build and run the MCP server over stdio.
	srv := mcpserver.New(client)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: MCP server exited: %v\n", err)
		os.Exit(1)
	}
}
