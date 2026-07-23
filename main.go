package main

import (
	"flag"
	"fmt"
	"log"

	"neuro-vsa/api"
)

func main() {
	port := flag.Int("port", 8080, "WebSocket API Server port")
	flag.Parse()

	fmt.Println("=======================================================================")
	fmt.Println("   NEURO-VSA (PhoneForge) — Bare-Metal Hyperdimensional Computing Engine")
	fmt.Println("   Zero-LLM | Zero-PyTorch | 10,000-Bit Hardware Bitwise Math Core")
	fmt.Println("=======================================================================")

	server := api.NewServer(*port)
	if err := server.Start(); err != nil {
		log.Fatalf("Fatal: WebSocket server crashed: %v", err)
	}
}
