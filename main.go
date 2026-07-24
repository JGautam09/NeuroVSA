package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/JGautam09/NeuroVSA/api"
	"github.com/JGautam09/NeuroVSA/parser"
)

func main() {
	port := flag.Int("port", 8080, "WebSocket API Server port")
	allowAllOrigins := flag.Bool("allow-all-origins", false, "Allow WebSocket connections from any origin (default: loopback only)")
	indexRoot := flag.String("index-root", ".", "Root directory the /ast indexer is confined to")
	astEncoder := flag.Int("ast-encoder", parser.EncoderV2, "AST encoder version: 1 = names only (legacy), 2 = structural (types + statement kinds + control flow)")
	flag.Parse()

	api.AllowAllOrigins = *allowAllOrigins

	fmt.Println("=======================================================================")
	fmt.Println("   NeuroVSA — From-scratch Hyperdimensional Computing (HDC/VSA) Engine")
	fmt.Println("   Zero external ML deps | 10,000-bit bitwise VSA core | CPU-only")
	fmt.Println("=======================================================================")

	server := api.NewServer(*port)
	if *astEncoder != parser.EncoderV1 && *astEncoder != parser.EncoderV2 {
		log.Fatalf("invalid -ast-encoder %d (want 1 or 2)", *astEncoder)
	}
	server.ASTIndexer.Version = *astEncoder
	server.IndexRoot = *indexRoot
	if err := server.Start(); err != nil {
		log.Fatalf("Fatal: WebSocket server crashed: %v", err)
	}
}
