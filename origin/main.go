package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	xconfig "code.uber.internal/go-common.git/x/config"

	"code.uber.internal/go-common.git/x/log"
	"code.uber.internal/infra/kraken/lib/peercontext"
	"code.uber.internal/infra/kraken/lib/store"
	"code.uber.internal/infra/kraken/lib/torrent"
	"code.uber.internal/infra/kraken/metrics"
	"code.uber.internal/infra/kraken/origin/blobserver"
)

func main() {
	announceIP := flag.String("announce_ip", "", "ip which peer will announce itself as")
	announcePort := flag.Int("announce_port", 0, "port which peer will announce itself as")
	flag.Parse()

	var config Config
	if err := xconfig.Load(&config); err != nil {
		panic(err)
	}

	// Disable JSON logging because it's completely unreadable.
	formatter := true
	config.Logging.TextFormatter = &formatter
	log.Configure(&config.Logging, false)

	// Initialize and start P2P scheduler client:

	pctx, err := peercontext.New(
		peercontext.PeerIDFactory(config.Torrent.PeerIDFactory), *announceIP, *announcePort)
	if err != nil {
		log.Fatalf("Failed to create peer context: %s", err)
	}

	stats, closer, err := metrics.New(config.Metrics)
	if err != nil {
		log.Fatalf("Failed to init metrics: %s", err)
	}
	defer closer.Close()

	fileStore, err := store.NewLocalFileStore(&config.LocalStore, true)
	if err != nil {
		log.Fatalf("Failed to create local store: %s", err)
	}

	client, err := torrent.NewSchedulerClient(&config.Torrent, fileStore, stats, pctx)
	if err != nil {
		log.Fatalf("Failed to create scheduler client: %s", err)
		panic(err)
	}
	defer client.Close()

	// The code below starts Blob HTTP server.
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("Error getting hostname: %s", err)
	}

	blobClientProvider := blobserver.NewHTTPClientProvider(config.BlobClient)

	server, err := blobserver.New(config.BlobServer, hostname, fileStore, blobClientProvider, pctx)
	if err != nil {
		log.Fatalf("Error initializing blob server %s: %s", hostname, err)
	}

	_, port, err := net.SplitHostPort(server.Addr())
	if err != nil {
		log.Fatalf("Failed to get port from addr %q: %s", server.Addr(), err)
	}

	log.Infof("Starting origin server %s on port %s", hostname, port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), server.Handler()))
}