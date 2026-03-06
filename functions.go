package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
)

var (
	TroveManagerAddrV1 = w3.A("0xA39739EF8b0231DbFA0DcdA07d7e29faAbCf4bb2")
	SortedTrovesAddrV1 = w3.A("0x8FdD3fbFEb32b28fb73555518f8b361bCeA741A6")
	TroveManagerAddrV2 = w3.A("0x7bcb64b2c9206a5b699ed43363f6f98d4776cf5a")
	SortedTrovesAddrV2 = w3.A("0xa25269e41bd072513849f2e64ad221e84f3063f4")
	funcGetFirstTrove  = w3.MustNewFunc("getFirst()", "address")
	funcGetLastTrove   = w3.MustNewFunc("getLast()", "address")
	funcGetNextTrove   = w3.MustNewFunc("getNext(address)", "address")
	funcGetPrevTrove   = w3.MustNewFunc("getPrev(address _id)", "address")
	funcGetSize        = w3.MustNewFunc("getSize()", "uint256")
	funcGetNominalICR  = w3.MustNewFunc("getNominalICR(address)", "uint256")
	funcGetTroveStatus = w3.MustNewFunc("getTroveStatus(address)", "uint256")
)

type Trove struct {
	Owner      common.Address
	NominalICR *big.Int
	Status     *big.Int
}

func GetSortedSize(client *w3.Client) (*big.Int, error) {
	ctx := context.Background()

	// 1. Récupérer le nombre de troves
	var size *big.Int
	if err := client.CallCtx(ctx,
		eth.CallFunc(SortedTrovesAddr, funcGetSize).Returns(&size),
	); err != nil {
		return nil, fmt.Errorf("getSize failed: %w", err)
	}
	return size, nil
}
func GetFirstTrove(client *w3.Client) (common.Address, error) {
	ctx := context.Background()

	// 1. Récupérer le nombre de troves
	var t common.Address
	if err := client.CallCtx(ctx,
		eth.CallFunc(SortedTrovesAddr, funcGetFirstTrove).Returns(&t),
	); err != nil {
		return t, fmt.Errorf("get trove: %w", err)
	}
	return t, nil
}
func GetLastTrove(client *w3.Client) (common.Address, error) {
	ctx := context.Background()

	// 1. Récupérer le nombre de troves
	var t common.Address
	if err := client.CallCtx(ctx,
		eth.CallFunc(SortedTrovesAddr, funcGetFirstTrove).Returns(&t),
	); err != nil {
		return t, fmt.Errorf("get trove: %w", err)
	}
	return t, nil
}

func GetLeastCollateralized(client *w3.Client, n int) ([]Trove, error) {
	ctx := context.Background()

	// 1. Récupérer le dernier trove (le moins collatéralisé)
	lastTrove, err := GetLastTrove(client)

	if err != nil {
		return nil, err
	}
	troves := make([]Trove, 0, n)
	current := lastTrove
	zeroAddr := common.Address{}

	for current != zeroAddr && len(troves) < n {
		var (
			nominalICR *big.Int
			status     *big.Int
			next       common.Address
		)

		// Batch les 3 appels en une seule requête RPC
		if err := client.CallCtx(ctx,
			eth.CallFunc(TroveManagerAddr, funcGetNominalICR, current).Returns(&nominalICR),
			eth.CallFunc(TroveManagerAddr, funcGetTroveStatus, current).Returns(&status),
			eth.CallFunc(SortedTrovesAddr, funcGetNextTrove, current).Returns(&next),
		); err != nil {
			return nil, err
		}

		// Status 1 = trove actif uniquement
		if status.Cmp(big.NewInt(1)) == 0 {
			troves = append(troves, Trove{
				Owner:      current,
				NominalICR: nominalICR,
				Status:     status,
			})
		}

		current = next
	}

	return troves, nil
}




type Branch struct {
    Name        string
    MCR         *big.Int
    TroveManager common.Address
    SortedTroves common.Address
    StabilityPool common.Address
}

var branches = []Branch{
    {
        Name:         "WETH",
        MCR:          mcrFromPercent(110),
        TroveManager: common.HexToAddress("0x..."),
        SortedTroves: common.HexToAddress("0x..."),
    },
    {
        Name:         "wstETH",
        MCR:          mcrFromPercent(120),
        TroveManager: common.HexToAddress("0x..."),
        SortedTroves: common.HexToAddress("0x..."),
    },
    {
        Name:         "rETH",
        MCR:          mcrFromPercent(120),
        TroveManager: common.HexToAddress("0x..."),
        SortedTroves: common.HexToAddress("0x..."),
    },
}

// Les fonctions prennent un uint256 troveId maintenant
var (
    funcGetCurrentICR    = w3.MustNewFunc("getCurrentICR(uint256,uint256)", "uint256")
    funcGetTroveEntireDebt = w3.MustNewFunc("getTroveEntireDebt(uint256)", "uint256")
    funcBatchLiquidate   = w3.MustNewFunc("batchLiquidateTroves(uint256[])", "")
    funcSortedGetNext    = w3.MustNewFunc("getNext(uint256)", "uint256") // uint256 pas address
)
