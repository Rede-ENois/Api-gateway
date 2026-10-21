package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"enois-gateway/internal/gateway"
	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "gateway.yaml", "path to the gateway YAML configuration")
	flag.Parse()

	file, err := os.Open(*configPath)
	if err != nil {
		log.Fatalf("open configuration: %v", err)
	}
	defer file.Close()
	var cfg gateway.Config
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		log.Fatalf("decode configuration: %v", err)
	}

	g, err := gateway.New(cfg)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}
	defer g.Close()

	srv := &http.Server{Addr: cfg.Listen, Handler: g, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: gateway.WriteTimeout, IdleTimeout: 90 * time.Second}
	go func() {
		log.Printf("gateway listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
