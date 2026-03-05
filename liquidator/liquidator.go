package liquidator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/lmittmann/w3"

	"liquity-keeper/internal/config"
	"liquity-keeper/internal/contracts"
	"liquity-keeper/internal/gas"
	"liquity-keeper/internal/watcher"
)

type Liquidator struct {
	w3Client   *w3.Client
	ethClient  *ethclient.Client
	gasEst     *gas.Estimator
	cfg        *config.Config
	privateKey *ecdsa.PrivateKey
	address    common.Address
	chainID    *big.Int
}

func New(w3Client *w3.Client, ethClient *ethclient.Client, cfg *config.Config) (*Liquidator, error) {
	pk, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("private key invalide: %w", err)
	}

	addr := crypto.PubkeyToAddress(pk.PublicKey)

	chainID, err := ethClient.ChainID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("chainID: %w", err)
	}

	log.Printf("[liquidator] keeper=%s chainID=%s", addr.Hex(), chainID.String())

	return &Liquidator{
		w3Client:   w3Client,
		ethClient:  ethClient,
		gasEst:     gas.NewEstimator(w3Client),
		cfg:        cfg,
		privateKey: pk,
		address:    addr,
		chainID:    chainID,
	}, nil
}

// ProcessBatch décide quelle stratégie utiliser et liquide
func (l *Liquidator) ProcessBatch(ctx context.Context, troves []watcher.TroveInfo) error {
	if len(troves) == 0 {
		return nil
	}

	gasInfo, err := l.gasEst.FetchGasInfo(ctx)
	if err != nil {
		return fmt.Errorf("gas info: %w", err)
	}

	// Vérifier fee cap maximum
	maxFee := gas.GweiToWei(l.cfg.MaxGasFeeGwei)
	if gasInfo.FeeCap.Cmp(maxFee) > 0 {
		log.Printf("[liquidator] ⛽ feeCap=%.1f gwei > max=%d gwei — skip",
			gas.WeiToGwei(gasInfo.FeeCap), l.cfg.MaxGasFeeGwei)
		return nil
	}

	// Calculer la rentabilité estimée
	// Récompense keeper Liquity: 200 LUSD + 0.5% ETH collatéral par batch
	// Ici on vérifie juste que le gas est raisonnable
	gasCost := gas.EstimateBatchGasCost(gasInfo, len(troves))
	log.Printf("[liquidator] feeCap=%.1f gwei | gasCost=%s wei | troves=%d",
		gas.WeiToGwei(gasInfo.FeeCap), gasCost.String(), len(troves))

	// Extraire les adresses
	addrs := make([]common.Address, len(troves))
	for i, t := range troves {
		addrs[i] = t.Owner
	}

	// Limiter par batch max
	if len(addrs) > l.cfg.MaxTrovesPerBatch {
		addrs = addrs[:l.cfg.MaxTrovesPerBatch]
	}

	return l.sendBatchLiquidate(ctx, addrs, gasInfo)
}

// sendBatchLiquidate envoie une tx batchLiquidateTroves
func (l *Liquidator) sendBatchLiquidate(
	ctx context.Context,
	troves []common.Address,
	gasInfo *gas.GasInfo,
) error {
	data, err := contracts.FuncBatchLiquidateTroves.EncodeArgs(troves)
	if err != nil {
		return fmt.Errorf("encode batchLiquidateTroves: %w", err)
	}

	tx, err := l.buildAndSend(ctx, config.AddrTroveManager, data, gasInfo,
		gas.GasLiquidateFirst+gas.GasLiquidatePerExtra*int64(len(troves)))
	if err != nil {
		return err
	}

	log.Printf("[liquidator] ✅ batchLiquidate tx=%s troves=%d", tx.Hash().Hex(), len(troves))
	return nil
}

// sendSingleLiquidate — fallback pour un seul trove
func (l *Liquidator) sendSingleLiquidate(
	ctx context.Context,
	trove common.Address,
	gasInfo *gas.GasInfo,
) error {
	data, err := contracts.FuncLiquidate.EncodeArgs(trove)
	if err != nil {
		return fmt.Errorf("encode liquidate: %w", err)
	}

	tx, err := l.buildAndSend(ctx, config.AddrTroveManager, data, gasInfo, gas.GasLiquidateSingle)
	if err != nil {
		return err
	}

	log.Printf("[liquidator] ✅ liquidate tx=%s trove=%s", tx.Hash().Hex(), trove.Hex())
	return nil
}

// buildAndSend construit une tx EIP-1559, la signe et l'envoie
func (l *Liquidator) buildAndSend(
	ctx context.Context,
	to common.Address,
	data []byte,
	gasInfo *gas.GasInfo,
	gasLimit int64,
) (*types.Transaction, error) {
	nonce, err := l.ethClient.PendingNonceAt(ctx, l.address)
	if err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}

	tip := gas.GweiToWei(l.cfg.MinGasTipGwei)
	// Utiliser le tip suggéré s'il est plus grand
	if gasInfo.Tip.Cmp(tip) > 0 {
		tip = new(big.Int).Set(gasInfo.Tip)
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   l.chainID,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: gasInfo.FeeCap,
		Gas:       uint64(gasLimit),
		To:        &to,
		Data:      data,
	})

	signer := types.LatestSignerForChainID(l.chainID)
	signed, err := types.SignTx(tx, signer, l.privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign tx: %w", err)
	}

	if err := l.ethClient.SendTransaction(ctx, signed); err != nil {
		return nil, fmt.Errorf("send tx: %w", err)
	}

	return signed, nil
}
