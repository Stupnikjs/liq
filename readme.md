# Liquity Keeper Bot

Bot de liquidation pour Liquity v1 (Ethereum Mainnet) écrit en Go.

## Stack

| Lib | Usage |
|---|---|
| `lmittmann/w3` | Batch RPC calls — tous les reads en 1 requête |
| `go-ethereum` | `ethclient` pour SendTransaction, ChainID |

## Architecture

```
cmd/bot/main.go              # boucle principale (1 cycle / bloc)
internal/
  config/config.go           # adresses contrats, params depuis env
  contracts/liquity.go       # ABI signatures encodées pour w3
  gas/gas.go                 # estimation gas EIP-1559
  watcher/watcher.go         # scan des troves via batch calls w3
  liquidator/liquidator.go   # construction + envoi des txs
```

## Flux d'exécution

```
Chaque nouveau bloc (~12s)
  │
  ├─ FetchPrice()              lastGoodPrice() du PriceFeed
  ├─ IsRecoveryMode()          getTCR() + checkRecoveryMode()  [1 batch]
  │
  ├─ WalkSortedTroves()        getFirst() puis getNext() × N   [batch par 20]
  │   └─ SortedTroves est trié ICR croissant → les plus risqués sont en tête
  │
  ├─ Batch ICR calls           getCurrentICR() × N troves      [1 seul appel RPC]
  │   └─ Filtre ICR < 110% (normal) ou ICR < 150% (recovery mode)
  │
  ├─ FetchGasInfo()            eth_gasPrice + eth_getBlockByNumber [1 batch]
  │   └─ Skip si feeCap > MAX_GAS_FEE_GWEI
  │
  └─ batchLiquidateTroves()   1 seule tx pour tout le batch
```

## Setup

```bash
# 1. Copier et remplir les variables d'environnement
cp .env.example .env
# → éditer RPC_URL et PRIVATE_KEY

# 2. Charger l'env et lancer
source .env
make run

# Ou builder un binaire
make build
./bin/keeper
```

## Pourquoi w3 pour les reads ?

`lmittmann/w3` permet d'envoyer plusieurs appels `eth_call` en un seul
JSON-RPC batch. Sans ça, scanner 100 troves = 100 requêtes séquentielles.
Avec w3, c'est 1 requête → latence divisée par ~100.

```go
// Exemple: 3 appels en 1 requête RPC
err := client.CallCtx(ctx,
    w3.NewCall(addrPriceFeed, funcLastGoodPrice).Returns(&price),
    w3.NewCall(addrTroveManager, funcGetTCR).Args(price).Returns(&tcr),
    w3.NewCall(addrTroveManager, funcCheckRecoveryMode).Args(price).Returns(&recovery),
)
```

## Optimisations possibles

- **Nœud privé** (Erigon/Geth) : latence < 50ms vs 200ms+ Alchemy
- **eth_subscribe** : remplacer le polling par un subscription `newHeads`
- **Mempool watching** : détecter les gros dumps de prix avant confirmation
- **Flashbots** : envoyer via bundle pour éviter tout front-running
- **Gas bumping** : si tx stuck > 30s, replace-by-fee +10%

## Récompense keeper Liquity

Pour chaque liquidation déclenchée :
- **200 LUSD fixe** par appel
- **0.5% du collatéral ETH** liquidé

Pas besoin de capital — aucun flash loan requis, Liquity utilise
sa Stability Pool pour absorber la dette.
