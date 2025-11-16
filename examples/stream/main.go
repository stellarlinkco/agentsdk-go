package main

import (
	"context"
	"log"
	"time"

	"github.com/cexll/agentsdk-go/pkg/agent"
	"github.com/cexll/agentsdk-go/pkg/server"
)

func main() {
	ag, err := agent.New(agent.Config{Name: "stream-demo", DefaultContext: agent.RunContext{SessionID: "demo-session"}})
	if err != nil {
		log.Fatalf("new agent: %v", err)
	}

	log.Println("--- RunStream sample ---")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	events, err := ag.RunStream(ctx, "hello streaming world")
	if err != nil {
		log.Fatalf("run stream: %v", err)
	}
	for evt := range events {
		log.Printf("event=%s data=%v", evt.Type, evt.Data)
	}

	log.Println("--- Starting HTTP/SSE server ---")
	srv := server.New(ag)
	go func() {
		if err := srv.ListenAndServe(":8080"); err != nil {
			log.Fatalf("http server: %v", err)
		}
	}()

	log.Println("POST /run        -> curl -X POST http://localhost:8080/run -d '{\"input\":\"demo\"}'")
	log.Println("GET  /run/stream -> curl -N http://localhost:8080/run/stream?input=hello")

	select {}
}
