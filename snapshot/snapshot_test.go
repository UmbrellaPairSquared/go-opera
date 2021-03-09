package snapshot

import (
	"testing"

	"bytes"
	"time"
	"math/big"
	randModule "math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

var rand = randModule.New(randModule.NewSource(time.Now().Unix()))

func randomInt() (i *big.Int) {
  uintMax, _ := big.NewInt(0).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
  i = big.NewInt(0).Rand(rand, uintMax)
  return
}

func randomHash() (hash common.Hash) {
	rand.Read(hash[:])
  return
}

func randomAddress() (addr common.Address) {
	rand.Read(addr[:])
  return
}

func TestSnaphot(t *testing.T) {
  db := rawdb.NewMemoryDatabase()
  stateWrappedDB := state.NewDatabase(db)
  statedb, err := state.New(common.Hash{}, stateWrappedDB, nil)
  if err != nil {
    t.Fatalf("Couldn't create a state %s", err)
  }

  addresses := make([]common.Address, 0)
  balances := make(map[common.Address]*big.Int)
	nonces := make(map[common.Address]uint64)
  code := make(map[common.Address][]byte)
  storage := make(map[common.Address]map[common.Hash]common.Hash)

  for a := 0; a < 200; a++ {
    // Create a new account every couple of cycles
    if a == 0 || (rand.Int() % 2 == 0) {
      addresses = append(addresses, randomAddress())
    }

    for a := 0; a < 30; a++ {
      // Randomly select accounts for action
      // Overlap is fine and even beneficial
      addr := addresses[rand.Int() % len(addresses)]

			// Set a random nonce
      if rand.Int() % 4 == 0 {
				nonce := rand.Uint64()
				statedb.SetNonce(addr, nonce)
				nonces[addr] = nonce
      }

      // Random balance
      if rand.Int() % 3 == 0 {
        balance := randomInt()
        // Chance to clear it, which will caused it to be ignored in the snapshot
        if rand.Int() % 8 == 0 {
          balance = big.NewInt(0)
        }
        statedb.SetBalance(addr, balance)
        balances[addr] = balance
      }

      // Generate code if there isn't already code
      if code[addr] == nil && (rand.Int() % 10 == 0) {
        code[addr] = make([]byte, (rand.Int() % 204800) + 1)
        rand.Read(code[addr])
        statedb.SetCode(addr, code[addr])
        storage[addr] = make(map[common.Hash]common.Hash)
      }

      // Create state elements if this is a contract
      if code[addr] != nil {
        q := rand.Int() % 10
        for i := 0; i < q; i++ {
          key := randomHash()
          value := randomHash()
          storage[addr][key] = value
          statedb.SetState(addr, key, value)
        }
      }
    }

    // If this the first round, set account 0 to have a balance of 0
    // If it's the second, use 1
    // Most basic values, so explicitly testing them...
    if a == 0 {
      statedb.SetBalance(addresses[0], big.NewInt(0))
      balances[addresses[0]] = big.NewInt(0)
    } else if a == 1 {
      statedb.SetBalance(addresses[0], big.NewInt(1))
      balances[addresses[0]] = big.NewInt(1)
    }

    // Commit the state
    root, err := statedb.Commit(true)
    if err != nil {
      t.Fatalf("%s", err)
    }

    // Create the snapshot
    snap := new(bytes.Buffer)
    err = NewSnapshot(statedb, randomHash(), root, snap)
    if err != nil {
      t.Fatalf("%s", err)
    }

    // Load it into a fresh state
    freshDB := rawdb.NewMemoryDatabase()
    freshStateWrappedDB := state.NewDatabase(freshDB)
    freshState, err := state.New(common.Hash{}, freshStateWrappedDB, nil)
    if err != nil {
      t.Fatalf("Couldn't create a fresh state %s", err)
    }
    root, err = Restore(freshState, bytes.NewReader(snap.Bytes()), 0)
    if err != nil {
      t.Fatalf("Couldn't restore the snapshot %s", err)
    }
    snap = nil

    // Verify equivalency
    for _, addr := range addresses {
      if statedb.GetBalance(addr).Cmp(freshState.GetBalance(addr)) != 0 {
        t.Fatalf("Balance is wrong")
      }

      if statedb.GetNonce(addr) != freshState.GetNonce(addr) {
        t.Fatalf("Nonce is wrong")
      }

      if bytes.Compare(statedb.GetCode(addr), freshState.GetCode(addr)) != 0 {
        t.Fatalf("Code is wrong")
      }

      for k, v := range storage[addr] {
        if statedb.GetState(addr, k) != v {
          t.Fatalf("Testing methodology is wrong")
        }
        if freshState.GetState(addr, k) != v {
          t.Fatalf("Storage variable is wrong")
        }
      }
    }
  }
}
