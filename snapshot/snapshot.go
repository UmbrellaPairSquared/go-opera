package snapshot

import (
  "errors"
  "io"
  "bufio"
  "bytes"
  "encoding/binary"
  "math/big"

  "github.com/ethereum/go-ethereum/common"
  "github.com/ethereum/go-ethereum/crypto"
  "github.com/ethereum/go-ethereum/rlp"
  "github.com/ethereum/go-ethereum/core/state"
)

// Account code hash used when there is no code
var noCode = common.BytesToHash(crypto.Keccak256(nil))

// Create a new snapshot using a State Database instance and header
// The header is only used for its hash and for the root used to access the state
func NewSnapshot(statedb *state.StateDB, header, root common.Hash, file io.Writer) (error) {
  db := statedb.Database()
  trie, err := db.OpenTrie(root)
  if err != nil {
    return err
  }

  // All errors after this shouldn't happen
  // We solely access and parse referenced data in the intended manner

  // Used to ID where on the old chain we left off
  // The root alone would do this, as it's the culmination of state
  // That said, a root can't be entered onto a blockchain explorer; a header hash can be
  // Useful for working with the old chain
  _, err = file.Write(header[:])
  if err != nil {
    return err
  }

  first := true
  it := trie.NodeIterator(nil)
  for it.Next(true) {
    if !it.Leaf() {
      continue
    }

    // Get the address. We could work with the address hash, which we've done historically
    // That said, this actually saves 12 bytes per account, and it greatly increases ease of work
    // Also necessary for full key tracking
    addr := common.BytesToAddress(trie.GetKey(it.LeafKey()))

    // Read the account
    acc := state.Account{}
    err = rlp.Decode(bytes.NewReader(it.LeafBlob()), &acc)
    if err != nil {
      return err
    }

    // Any commit(true) will delete empty accounts, so this shouldn't be necessary
    // Furthermore, our checks to only include values with meaning make this even more redundant
    // That said, it's a good check to ensure a minimal state size
    if statedb.HasSuicided(addr) || statedb.Empty(addr) {
      continue
    }

    // Write 0 to say this object is starting
    // As the last field is of a variable segmented size, this is needed
    // We don't RLP encode the entire thing as that requires loading it
    // We could, yet it's better to minimize RAM usage than to maintain convention
    // Convention would also produce a larger output (multi-byte lengths)

    // Not necessary for the first address
    if !first {
      _, err = file.Write([]byte{0: 0})
      if err != nil {
        return err
      }
    }
    first = false

    // Write the address
    // Doesn't RLP encode due to the fixed length
    _, err = file.Write(addr[:])
    if err != nil {
      return err
    }

    // With the RAM proof of concept, there was a check for this being non-0
    // That said, the file needs some value to denote if a balance exists, and this does encode in a minimal form
    // We could prefix a flag byte for the account, which would save one byte for non-contracts
    // (it'd say balance is present and no code/storage)
    // A technical optimization which likely isn't worth the added complexity
    // Especially because for contracts with a balance, it'd lose a byte, further reducing its efficiency
    err = rlp.Encode(file, acc.Balance)
    if err != nil {
      return err
    }

    // This originally wasn't included due to not being needed by the new chain
    // That said, it still is necessary to stop replay attacks
    // An offset on the new chain would fix this, but it's better to include such bytes in the snapshot
    err = rlp.Encode(file, acc.Nonce)
    if err != nil {
      return err
    }

    codeHash := common.BytesToHash(acc.CodeHash)
    if codeHash == noCode {
      err = rlp.Encode(file, []byte{})
      if err != nil {
        return err
      }
      continue
    }

    // Passes nil-equivalent for address hash as it isn't actually used
    // Skips a keccak run to calculate the address hash
    code, err := db.ContractCode(common.Hash{}, codeHash)
    if err != nil {
      return err
    }
    err = rlp.Encode(file, code)
    if err != nil {
      return err
    }

    // Iterate over the contract storage
    dataTrie, err := db.OpenStorageTrie(common.BytesToHash(it.LeafKey()), acc.Root)
    if err != nil {
      return err
    }
    data := dataTrie.NodeIterator(nil)
    for data.Next(true) {
      if !data.Leaf() {
        continue
      }

      // Write 1 to say there is data
      _, err = file.Write([]byte{0: 1})
      if err != nil {
        return err
      }

      key := dataTrie.GetKey(data.LeafKey())
      _, content, _, _ := rlp.Split(data.LeafBlob())

      // Write the key and content
      // Despite the StateDB ensuring these are hashes, leading 0s are pruned
      // That's why these are RLP encoded, or we could use a fixed size of 32-bytes
      err = rlp.Encode(file, key)
      if err != nil {
        return err
      }
      err = rlp.Encode(file, content)
      if err != nil {
        return err
      }
    }
  }
  return nil
}

// Helper function for the one below
func getExtendedLength(file io.ByteReader, prefix []byte, offset byte) (itemLen int, newPrefix []byte, err error) {
  secondLength := make([]byte, 8)
  for i := byte(0); i < prefix[0] - offset; i++ {
    secondLength[(8 - (prefix[0] - offset)) + i], err = file.ReadByte()
    if err != nil {
      return 0, nil, err
    }
  }

  // Safe until 2 GB contracts are deployed
  itemLen = int(binary.BigEndian.Uint64(secondLength))
  newPrefix = append(prefix, secondLength[(8 - (prefix[0] - offset)):]...)
  return
}

// Gets the next RLP segement
func getNextRLP(file io.ByteReader) ([]byte, error) {
  value, err := file.ReadByte()
  if err != nil {
    return nil, err
  }
  prefix := []byte{0: value}

  itemLen := 0
  if 0xf8 <= prefix[0] {
    itemLen, prefix, err = getExtendedLength(file, prefix, 0xf7)
    if err != nil {
      return nil, err
    }
  } else if 0xc0 <= prefix[0] {
    itemLen = int(prefix[0] - 0xc0)
  } else if 0xb8 <= prefix[0] {
    itemLen, prefix, err = getExtendedLength(file, prefix, 0xb7)
    if err != nil {
      return nil, err
    }
  } else if 0x80 <= prefix[0] {
    itemLen = int(prefix[0] - 0x80)
  } else {
    itemLen = 0
  }

  if itemLen == 0 {
    return prefix, nil
  }

  res := make([]byte, itemLen)
  for i := 0; i < itemLen; i++ {
    res[i], err = file.ReadByte()
    if err != nil {
      return nil, err
    }
  }
  res = append(prefix, res...)
  return res, nil
}

func readFull(file io.ByteReader, data []byte) (err error) {
  for i := 0; i < len(data); i++ {
    data[i], err = file.ReadByte()
    if err != nil {
      return err
    }
  }
  return nil
}

func Restore(statedb *state.StateDB, file io.Reader, maxMemoryUsage int) (common.Hash, error) {
  mem := 0
	capEvm := func (usage int) {
    _ = usage
  }
  if maxMemoryUsage != 0 {
    capEvm = func(usage int) {
  		mem += usage
  		if mem > maxMemoryUsage {
  			_, _ = statedb.Commit(true)
  			_ = statedb.Database().TrieDB().Cap(common.StorageSize(maxMemoryUsage / 8))
  			mem = 0
  		}
  	}
  }

  reader := bufio.NewReader(file)

  // Move forward 32 bytes for the header hash
  header := make([]byte, 32)
  err := readFull(reader, header)
  if err != nil {
    return common.Hash{}, err
  }

  for true {
    // Read the address
    addr := common.Address{}
    err = readFull(reader, addr[:])
    if err != nil {
      return common.Hash{}, err
    }

    // Read and set the balance
    balance := big.NewInt(0)
    next, err := getNextRLP(reader)
    if err != nil {
      return common.Hash{}, err
    }
    err = rlp.DecodeBytes(next, balance)
    if err != nil {
      return common.Hash{}, err
    }
    statedb.SetBalance(addr, balance)
    // These values were taken from elsewhere in the codebase
    // They should be overkill for balance/nonce, yet higher doesn't hurt RAM usage
    // They also may be accurate, in which case they're needed
    // If they are wrong, a minor penalty to performance when loading will be incurred
    capEvm(512)

    // Read and set the nonce
    nonce := uint64(0)
    next, err = getNextRLP(reader)
    if err != nil {
      return common.Hash{}, err
    }
    err = rlp.DecodeBytes(next, &nonce)
    if err != nil {
      return common.Hash{}, err
    }
    statedb.SetNonce(addr, nonce)
    capEvm(512)

    // Read and set the code
    code := []byte{}
    next, err = getNextRLP(reader)
    if err != nil {
      return common.Hash{}, err
    }
    err = rlp.DecodeBytes(next, &code)
    if err != nil {
      return common.Hash{}, err
    }
    statedb.SetCode(addr, code)
    capEvm(len(code))

    // Read and set every storage variable, for as long as there is one
    for true {
      continuation, err := reader.ReadByte()
      if err != nil {
        // Break if this is the end of the file
        if err == io.EOF {
          return statedb.Commit(true)
        }
        return common.Hash{}, err
      }
      if continuation == 0 {
        break
      }

      pair := make([][]byte, 2)
      for i := range pair {
        next, err = getNextRLP(reader)
        if err != nil {
          return common.Hash{}, err
        }
        err = rlp.DecodeBytes(next, &pair[i])
        if err != nil {
          return common.Hash{}, err
        }
      }
      statedb.SetState(addr, common.BytesToHash(pair[0]), common.BytesToHash(pair[1]))
    	capEvm(512)
    }
  }

  return common.Hash{}, errors.New("Restore broke loop before EOF")
}
