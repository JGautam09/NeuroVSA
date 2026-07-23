package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/JGautam09/NeuroVSA/api"
)

func main() {
	port := flag.Int("port", 8080, "WebSocket API Server port")
	allowAllOrigins := flag.Bool("allow-all-origins", false, "Allow WebSocket connections from any origin (default: loopback only)")
	indexRoot := flag.String("index-root", ".", "Root directory the /ast indexer is confined to")
	flag.Parse()

	api.AllowAllOrigins = *allowAllOrigins

	fmt.Println("=======================================================================")
	fmt.Println("   NeuroVSA — From-scratch Hyperdimensional Computing (HDC/VSA) Engine")
	fmt.Println("   Zero external ML deps | 10,000-bit bitwise VSA core | CPU-only")
	fmt.Println("=======================================================================")

	server := api.NewServer(*port)
	server.IndexRoot = *indexRoot
	if err := server.Start(); err != nil {
		log.Fatalf("Fatal: WebSocket server crashed: %v", err)
	}
}
