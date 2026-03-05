package config

import (
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
)

// Liquity v1 Mainnet contract addresses
var (
	AddrTroveManager  = common.HexToAddress("0xA39739EF8b0231DbFA0DcdA07d7e29faAbCf4bb2")
	AddrSortedTroves  = common.HexToAddress("0x8FdD3fbFEb32b28fb73555518f8b361bCeA741A6")
	AddrPriceFeed     = common.HexToAddress("0x4c517D4e2C851CA76d7eC94B805269Df0f2201De")
	AddrStabilityPool = common.HexToAddress("0x66017D22b0f8556afDd19FC67041899Eb65a21bb")
	AddrLUSD          = common.HexToAddress("0x5f98805A4E8be255a32880FDeC7F6728C6568bA0")
)

// Liquity constants
var (
	// MCR = 110% — Minimum Collateralization Ratio
	// En dessous → le trove est liquidable
	MCR = new(big.Int).Mul(big.NewInt(11), new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil))

	// CCR = 150% — Critical Collateralization Ratio (Recovery Mode)
	CCR = new(big.Int).Mul(big.NewInt(15), new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil))

	// Compensation keeper: 200 LUSD fixe + 0.5% du collatéral
	KeeperLUSDReward  = new(big.Int).Mul(big.NewInt(200), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	KeeperCollPercent = big.NewInt(5) // 0.5% = 5/1000
	KeeperCollDivisor = big.NewInt(1000)
)

type Config struct {
	RPCURL     string
	PrivateKey string // hex sans 0x

	// Polling
	PollBlocks int // tous les N blocs

	// Limites
	MaxTrovesPerScan  int   // combien de troves à scanner par cycle
	MaxTrovesPerBatch int   // max troves par batchLiquidate tx
	MinGasTipGwei     int64 // tip EIP-1559 minimum
	MaxGasFeeGwei     int64 // fee cap maximum — on skip si trop cher
}

func Load() (*Config, error) {
	rpc := os.Getenv("RPC_URL")
	if rpc == "" {
		return nil, fmt.Errorf("RPC_URL manquant")
	}
	pk := os.Getenv("PRIVATE_KEY")
	if pk == "" {
		return nil, fmt.Errorf("PRIVATE_KEY manquant")
	}

	cfg := &Config{
		RPCURL:            rpc,
		PrivateKey:        pk,
		PollBlocks:        1,
		MaxTrovesPerScan:  100,
		MaxTrovesPerBatch: 40,
		MinGasTipGwei:     1,
		MaxGasFeeGwei:     intEnv("MAX_GAS_FEE_GWEI", 50),
	}

	log.Printf("[config] MaxGasFee=%d gwei | MaxTrovesScan=%d | MaxTrovesBatch=%d",
		cfg.MaxGasFeeGwei, cfg.MaxTrovesPerScan, cfg.MaxTrovesPerBatch)

	return cfg, nil
}

func intEnv(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}
