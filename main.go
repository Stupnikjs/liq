package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
)

var (
	dRPC   = "https://lb.drpc.live/ethereum/AhuxMhCqfkI8pF_0y4Fpi89GWcIMFIwR8ZsatuZZzRRv"
	pubRPC = ""
	// Chainlink ETH/USD feed mainnet
	ChainlinkETHUSD  = w3.A("0x5f4eC3Df9cbd43714FE2740f5E3616155c5b8419")
	funcLatestAnswer = w3.MustNewFunc("latestAnswer()", "int256")
)

func main() {
	// Connexion au noeud Ethereum (remplace par ton RPC)
	client, err := w3.Dial(dRPC)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	troves, err := GetLeastCollateralized(client, 2000)
	if err != nil {
		panic(err)
	}
	ETHUSD, err := GetETHPrice(client)
	if err != nil {
		panic(err)
	}
	ethPrice18 := new(big.Int).Mul(ETHUSD, big.NewInt(1e10))
	for _, t := range troves {
		fmt.Println(NICRToPercent(t.NominalICR, ethPrice18))
	}
}

func GetETHPrice(client *w3.Client) (*big.Int, error) {
	ctx := context.Background()

	var price *big.Int
	if err := client.CallCtx(ctx,
		eth.CallFunc(ChainlinkETHUSD, funcLatestAnswer).Returns(&price),
	); err != nil {
		return nil, fmt.Errorf("getPrice failed: %w", err)
	}

	// Chainlink ETH/USD retourne 8 decimales
	// ex: 300000000000 = 3000.00000000 USD
	fmt.Printf("ETH Price: $%.2f\n", float64(price.Int64())/1e8)

	return price, nil
}

func NICRToPercent(nicr *big.Int, ethPrice18 *big.Int) float64 {
	// ICR = NICR * ethPrice / 100e18
	// ethPrice18 est en 18 decimales (ex: 2040 * 1e18)

	num := new(big.Int).Mul(nicr, ethPrice18)

	// Diviseur = 100e18 * 1e18 = 1e38
	// (100e18 pour annuler le NICR, 1e18 pour annuler le prix)
	e38 := new(big.Int).Exp(big.NewInt(10), big.NewInt(38), nil)

	icrFloat, _ := new(big.Float).Quo(
		new(big.Float).SetInt(num),
		new(big.Float).SetInt(e38),
	).Float64()

	return icrFloat * 100 // en %
}
