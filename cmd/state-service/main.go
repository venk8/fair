package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/satmihir/fair/pkg/broadcast"
	"github.com/satmihir/fair/pkg/state"
	statepb "github.com/satmihir/fair/pkg/state/api/v1"
	"github.com/satmihir/fair/pkg/state/store"
	"google.golang.org/grpc"
)

func main() {
	grpcAddr := flag.String("grpc-addr", ":50051", "gRPC listen address")
	metricsAddr := flag.String("metrics-addr", ":9090", "Metrics listen address")
	seedWindow := flag.Duration("seed-window", 5*time.Minute, "Seed window duration")
	evictionTTL := flag.Duration("eviction-ttl", 15*time.Minute, "Seed eviction TTL")
	flag.Parse()

	log.Printf("Starting State Service on %s", *grpcAddr)
	log.Printf("Metrics endpoint (not yet active) on %s", *metricsAddr)

	// Initialize components
	// Eviction tick every 1 minute
	sStore := store.NewInMemoryStore(*seedWindow, *evictionTTL, 1*time.Minute)
	hub := broadcast.NewHub()

	// Start background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sStore.Start(ctx)
	go hub.Run()

	// Create gRPC server
	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	stateService := state.NewService(sStore, hub)
	statepb.RegisterStateServiceServer(grpcServer, stateService)

	// Graceful shutdown handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down State Service...")
	grpcServer.GracefulStop()
	sStore.Stop()
	// Hub stops when main exits
}
