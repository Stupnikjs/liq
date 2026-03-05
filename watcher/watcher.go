package watcher

import (
	"context"
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
	"github.com/lmittmann/w3/w3types"

	"liquity-keeper/internal/config"
	"liquity-keeper/internal/contracts"
)

type TroveInfo struct {
	Owner common.Address
	ICR   *big.Int
}

type Watcher struct {
	client *w3.Client
	cfg    *config.Config
}

func New(client *w3.Client, cfg *config.Config) *Watcher {
	return &Watcher{client: client, cfg: cfg}
}

// FetchPrice retourne le dernier prix ETH/USD connu du PriceFeed Liquity (18 décimales)
func (w *Watcher) FetchPrice(ctx context.Context) (*big.Int, error) {
	var price big.Int
	err := w.client.CallCtx(ctx,
		w3.NewCall(config.AddrPriceFeed, contracts.FuncLastGoodPrice).Returns(&price),
	)
	if err != nil {
		return nil, fmt.Errorf("fetchPrice: %w", err)
	}
	return &price, nil
}

// FetchBlockNumber retourne le numéro du bloc courant
func (w *Watcher) FetchBlockNumber(ctx context.Context) (uint64, error) {
	var n big.Int
	if err := w.client.CallCtx(ctx, eth.BlockNumber(&n)); err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// IsRecoveryMode vérifie si le protocole est en Recovery Mode (TCR < 150%)
// En Recovery Mode, les troves ICR < 150% sont liquidables — plus de surface
func (w *Watcher) IsRecoveryMode(ctx context.Context, price *big.Int) (bool, *big.Int, error) {
	var tcr big.Int
	var recovery bool
	err := w.client.CallCtx(ctx,
		w3.NewCall(config.AddrTroveManager, contracts.FuncGetTCR).Args(price).Returns(&tcr),
		w3.NewCall(config.AddrTroveManager, contracts.FuncCheckRecoveryMode).Args(price).Returns(&recovery),
	)
	if err != nil {
		return false, nil, fmt.Errorf("checkRecoveryMode: %w", err)
	}
	return recovery, &tcr, nil
}

// WalkSortedTroves parcourt la liste SortedTroves du premier (plus risqué)
// et retourne les N premières adresses en batch calls groupés
func (w *Watcher) WalkSortedTroves(ctx context.Context, maxCount int) ([]common.Address, error) {
	// 1. Récupérer head + taille en un seul batch
	var first common.Address
	var size big.Int
	err := w.client.CallCtx(ctx,
		w3.NewCall(config.AddrSortedTroves, contracts.FuncSortedGetFirst).Returns(&first),
		w3.NewCall(config.AddrSortedTroves, contracts.FuncSortedGetSize).Returns(&size),
	)
	if err != nil {
		return nil, fmt.Errorf("sortedTroves head: %w", err)
	}

	total := int(size.Int64())
	if total == 0 || first == (common.Address{}) {
		return nil, nil
	}
	if maxCount > total {
		maxCount = total
	}

	troves := make([]common.Address, 0, maxCount)
	troves = append(troves, first)

	// 2. Parcourir par batch de getNext() — chaque batch = 1 appel RPC multicall
	const batchStep = 20
	current := first

	for len(troves) < maxCount {
		need := maxCount - len(troves)
		if need > batchStep {
			need = batchStep
		}

		nexts := make([]common.Address, need)
		calls := make([]w3types.RPCCaller, need)
		for i := range calls {
			calls[i] = w3.NewCall(
				config.AddrSortedTroves,
				contracts.FuncSortedGetNext,
			).Args(current).Returns(&nexts[i])
		}

		if err := w.client.CallCtx(ctx, calls...); err != nil {
			return nil, fmt.Errorf("sortedTroves walk: %w", err)
		}

		for _, next := range nexts {
			if next == (common.Address{}) {
				return troves, nil // fin de liste
			}
			troves = append(troves, next)
			current = next
			if len(troves) >= maxCount {
				break
			}
		}
	}

	return troves, nil
}

// ScanLiquidatable retourne les troves dont l'ICR < seuil de liquidation
// Effectue un batch call unique pour tous les ICR → très peu de requêtes RPC
func (w *Watcher) ScanLiquidatable(ctx context.Context) ([]TroveInfo, error) {
	// 1. Prix + état Recovery Mode en parallèle
	price, err := w.FetchPrice(ctx)
	if err != nil {
		return nil, err
	}

	recovery, tcr, err := w.IsRecoveryMode(ctx, price)
	if err != nil {
		return nil, err
	}

	// Seuil selon le mode
	threshold := new(big.Int).Set(config.MCR) // 110% en normal
	if recovery {
		threshold = new(big.Int).Set(config.CCR) // 150% en recovery
		log.Printf("[watcher] ⚠️  RECOVERY MODE — TCR=%.2f%% seuil=150%%",
			toPercent(tcr))
	}

	// 2. Récupérer les troves à scanner (début de liste = plus risqués)
	owners, err := w.WalkSortedTroves(ctx, w.cfg.MaxTrovesPerScan)
	if err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		return nil, nil
	}

	// 3. Batch call ICR pour tous les owners en une seule requête RPC
	icrs := make([]big.Int, len(owners))
	calls := make([]w3types.RPCCaller, len(owners))
	for i, owner := range owners {
		calls[i] = w3.NewCall(
			config.AddrTroveManager,
			contracts.FuncGetCurrentICR,
		).Args(owner, price).Returns(&icrs[i])
	}

	if err := w.client.CallCtx(ctx, calls...); err != nil {
		return nil, fmt.Errorf("batch getCurrentICR: %w", err)
	}

	// 4. Filtrer
	var liquidatable []TroveInfo
	for i, icr := range icrs {
		icrCopy := new(big.Int).Set(&icr)
		if icrCopy.Cmp(threshold) < 0 {
			liquidatable = append(liquidatable, TroveInfo{
				Owner: owners[i],
				ICR:   icrCopy,
			})
			log.Printf("[watcher] 🎯 liquidable: %s ICR=%.2f%%",
				owners[i].Hex(), toPercent(icrCopy))
		}
	}

	log.Printf("[watcher] scan=%d troves | liquidables=%d | prix=$%.2f | recovery=%v",
		len(owners), len(liquidatable), weiToUSD(price), recovery)

	return liquidatable, nil
}

func toPercent(v *big.Int) float64 {
	f, _ := new(big.Float).Quo(
		new(big.Float).SetInt(v),
		new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(16), nil)),
	).Float64()
	return f
}

func weiToUSD(v *big.Int) float64 {
	f, _ := new(big.Float).Quo(
		new(big.Float).SetInt(v),
		new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)),
	).Float64()
	return f
}
