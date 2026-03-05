package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/lmittmann/w3"

	"liquity-keeper/internal/config"
	"liquity-keeper/internal/liquidator"
	"liquity-keeper/internal/watcher"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("🤖 Liquity Keeper Bot — démarrage")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// w3.Client pour les batch calls (lecture)
	w3Client, err := w3.Dial(cfg.RPCURL)
	if err != nil {
		log.Fatalf("w3 dial: %v", err)
	}
	defer w3Client.Close()

	// ethclient standard pour SendTransaction + ChainID
	ethClient, err := ethclient.Dial(cfg.RPCURL)
	if err != nil {
		log.Fatalf("ethclient dial: %v", err)
	}
	defer ethClient.Close()

	watch := watcher.New(w3Client, cfg)

	liq, err := liquidator.New(w3Client, ethClient, cfg)
	if err != nil {
		log.Fatalf("liquidator: %v", err)
	}

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigs
		log.Printf("Signal %s reçu — arrêt propre...", s)
		cancel()
	}()

	// Boucle basée sur les blocs
	// On poll toutes les 12s (~1 bloc mainnet) et on skip si même bloc
	var lastBlock uint64
	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()

	log.Println("🔍 Surveillance des troves démarrée")

	for {
		select {
		case <-ctx.Done():
			log.Println("Bot arrêté.")
			return

		case <-ticker.C:
			blockNum, err := watch.FetchBlockNumber(ctx)
			if err != nil {
				log.Printf("[main] FetchBlockNumber: %v", err)
				continue
			}

			// Skip si pas de nouveau bloc
			if blockNum <= lastBlock {
				continue
			}
			lastBlock = blockNum

			log.Printf("📦 Bloc #%d", blockNum)

			if err := runCycle(ctx, watch, liq); err != nil {
				log.Printf("[main] cycle error: %v", err)
			}
		}
	}
}

func runCycle(ctx context.Context, watch *watcher.Watcher, liq *liquidator.Liquidator) error {
	t0 := time.Now()

	troves, err := watch.ScanLiquidatable(ctx)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	if len(troves) == 0 {
		log.Printf("[cycle] rien à liquider | %dms", time.Since(t0).Milliseconds())
		return nil
	}

	if err := liq.ProcessBatch(ctx, troves); err != nil {
		return fmt.Errorf("liquidate: %w", err)
	}

	log.Printf("[cycle] ✅ %d troves traités | %dms", len(troves), time.Since(t0).Milliseconds())
	return nil
}
