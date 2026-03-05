package gas

import (
	"context"
	"fmt"
	"math/big"

	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
)

// Coûts gas empiriques mesurés sur Liquity mainnet
const (
	GasLiquidateSingle   = int64(200_000) // liquidate(address)
	GasLiquidateFirst    = int64(200_000) // overhead fixe batchLiquidateTroves
	GasLiquidatePerExtra = int64(80_000)  // coût marginal par trove supplémentaire
)

type Estimator struct {
	client *w3.Client
}

func NewEstimator(client *w3.Client) *Estimator {
	return &Estimator{client: client}
}

type GasInfo struct {
	BaseFee *big.Int // baseFee du bloc courant
	Tip     *big.Int // priorityFee suggéré
	FeeCap  *big.Int // baseFee * 2 + tip (standard EIP-1559)
}

// FetchGasInfo récupère baseFee + priorityFee en un batch call
func (e *Estimator) FetchGasInfo(ctx context.Context) (*GasInfo, error) {
	var tip big.Int
	var block eth.Block

	err := e.client.CallCtx(ctx,
		eth.GasTipCap(&tip),
		eth.BlockByNumber(nil, &block), // nil = latest
	)
	if err != nil {
		return nil, fmt.Errorf("fetch gas info: %w", err)
	}

	baseFee := block.BaseFee
	if baseFee == nil {
		// pre-EIP1559 fallback
		baseFee = new(big.Int).Mul(big.NewInt(20), new(big.Int).Exp(big.NewInt(10), big.NewInt(9), nil))
	}

	// FeeCap = 2 * baseFee + tip (marge pour le prochain bloc)
	feeCap := new(big.Int).Add(
		new(big.Int).Mul(baseFee, big.NewInt(2)),
		&tip,
	)

	return &GasInfo{
		BaseFee: baseFee,
		Tip:     &tip,
		FeeCap:  feeCap,
	}, nil
}

// EstimateBatchGasCost calcule le coût total gas pour N troves
func EstimateBatchGasCost(gasInfo *GasInfo, nTroves int) *big.Int {
	if nTroves == 0 {
		return big.NewInt(0)
	}
	gasUnits := GasLiquidateFirst + GasLiquidatePerExtra*int64(nTroves-1)
	return new(big.Int).Mul(big.NewInt(gasUnits), gasInfo.FeeCap)
}

// GweiToWei convertit des gwei en wei
func GweiToWei(gwei int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(gwei), new(big.Int).Exp(big.NewInt(10), big.NewInt(9), nil))
}

// WeiToGwei pour l'affichage
func WeiToGwei(wei *big.Int) float64 {
	gwei := new(big.Float).Quo(
		new(big.Float).SetInt(wei),
		new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(9), nil)),
	)
	f, _ := gwei.Float64()
	return f
}
