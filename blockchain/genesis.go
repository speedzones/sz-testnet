// Copyright 2018 The sphinx Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The sphinx is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The sphinx is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the sphinx. If not, see <http://www.gnu.org/licenses/>.

package bc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/shx-project/sphinx/blockchain/state"
	"github.com/shx-project/sphinx/blockchain/storage"
	"github.com/shx-project/sphinx/blockchain/types"
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/common/hexutil"
	"github.com/shx-project/sphinx/common/log"
	"github.com/shx-project/sphinx/common/math"
	"github.com/shx-project/sphinx/config"
	"math/big"
)

//go:generate gencodec -type Genesis -field-override genesisSpecMarshaling -out gen_genesis.go

var errGenesisNoConfig = errors.New("genesis has no chain configuration")

// Genesis specifies the header fields, state of a genesis block. It also defines hard
// fork switch-over blocks through the chain configuration.
type Genesis struct {
	Config     *config.ChainConfig `json:"config"`
	Timestamp  uint64              `json:"timestamp"`
	ExtraData  []byte              `json:"extraData"`
	Difficulty *big.Int            `json:"difficulty" gencodec:"required"`
	Coinbase   common.Address      `json:"coinbase"`
	Number     uint64              `json:"number"`
	ParentHash common.Hash         `json:"parentHash"`
}

// GenesisAlloc specifies the initial state that is part of the genesis block.
type GenesisAlloc map[common.Address]GenesisAccount

// GenesisAccount is an account in the state of the genesis block.
type GenesisAccount struct {
	Storage    map[common.Hash]common.Hash `json:"storage,omitempty"`
	Nonce      uint64                      `json:"nonce,omitempty"`
	PrivateKey []byte                      `json:"secretKey,omitempty"` // for tests
}

type genesisAccountMarshaling struct {
	Nonce      math.HexOrDecimal64
	Storage    map[storageJSON]storageJSON
	PrivateKey hexutil.Bytes
}

// storageJSON represents a 256 bit byte array, but allows less than 256 bits when
// unmarshaling from hex.
type storageJSON common.Hash

func (h *storageJSON) UnmarshalText(text []byte) error {
	text = bytes.TrimPrefix(text, []byte("0x"))
	if len(text) > 64 {
		return fmt.Errorf("too many hex characters in storage key/value %q", text)
	}
	offset := len(h) - len(text)/2 // pad on the left
	if _, err := hex.Decode(h[offset:], text); err != nil {
		fmt.Println(err)
		return fmt.Errorf("invalid hex storage key/value %q", text)
	}
	return nil
}

func (h storageJSON) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

// GenesisMismatchError is raised when trying to overwrite an existing
// genesis block with an incompatible one.
type GenesisMismatchError struct {
	Stored, New common.Hash
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("database already contains an incompatible genesis block (have %x, new %x)", e.Stored[:8], e.New[:8])
}

func SetupGenesisBlock(db shxdb.Database, genesis *Genesis) (*config.ChainConfig, common.Hash, error) {
	if genesis != nil && genesis.Config == nil {
		return config.MainnetChainConfig, common.Hash{}, errGenesisNoConfig
	}

	// Just commit the new block if there is no stored genesis block.
	stored := GetCanonicalHash(db, 0)

	if (stored == common.Hash{}) {

		if genesis == nil {
			genesis = DefaultGenesisBlock()
		} else {
			log.Info("Writing custom genesis block")
		}
		block, err := genesis.Commit(db)
		return genesis.Config, block.Hash(), err
	}

	// Check whether the genesis block is already written.
	if genesis != nil {
		block, _ := genesis.ToBlock()
		hash := block.Hash()
		if hash != stored {
			return genesis.Config, block.Hash(), &GenesisMismatchError{stored, hash}
		}
	}

	// Get the existing chain configuration.
	newcfg := genesis.configOrDefault(stored)
	storedcfg, err := GetChainConfig(db, stored)
	if err != nil {
		if err == ErrChainConfigNotFound {
			// This case happens if a genesis write was interrupted.
			log.Warn("Found genesis block without chain config")
			err = WriteChainConfig(db, stored, newcfg)
		}
		return newcfg, stored, err
	}
	// Special case: don't change the existing config of a non-mainnet chain if no new
	// config is supplied. These chains would get AllProtocolChanges (and a compat error)
	// if we just continued here.
	if genesis == nil && stored != config.MainnetGenesisHash {
		return storedcfg, stored, nil
	}

	// Check config compatibility and write the config. Compatibility errors
	// are returned to the caller unless we're already at block zero.
	height := GetBlockNumber(db, GetHeadHeaderHash(db))
	if height == missingNumber {
		return newcfg, stored, fmt.Errorf("missing block number for head header hash")
	}
	compatErr := storedcfg.CheckCompatible(newcfg, height)
	if compatErr != nil && height != 0 && compatErr.RewindTo != 0 {
		return newcfg, stored, compatErr
	}
	return newcfg, stored, WriteChainConfig(db, stored, newcfg)
}

func (g *Genesis) configOrDefault(ghash common.Hash) *config.ChainConfig {
	return config.MainnetChainConfig
}

// ToBlock creates the block and state of a genesis specification.
func (g *Genesis) ToBlock() (*types.Block, *state.StateDB) {
	db, _ := shxdb.NewMemDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db))
	root := statedb.IntermediateRoot(false)
	head := &types.Header{
		Number:     new(big.Int).SetUint64(g.Number),
		Time:       new(big.Int).SetUint64(g.Timestamp),
		ParentHash: g.ParentHash,
		Extra:      g.ExtraData,
		Difficulty: g.Difficulty,
		Coinbase:   g.Coinbase,
		Root:       root,
	}
	if g.Difficulty == nil {
		head.Difficulty = config.GenesisDifficulty
	}
	return types.NewBlock(head, nil, nil, nil), statedb
}

// Commit writes the block and state of a genesis specification to the database.
// The block is committed as the canonical head block.
func (g *Genesis) Commit(db shxdb.Database) (*types.Block, error) {
	block, statedb := g.ToBlock()
	if block.Number().Sign() != 0 {
		return nil, fmt.Errorf("can't commit genesis block with number > 0")
	}
	if _, err := statedb.CommitTo(db, false); err != nil {
		return nil, fmt.Errorf("cannot write state: %v", err)
	}
	if err := WriteTd(db, block.Hash(), block.NumberU64(), g.Difficulty); err != nil {
		return nil, err
	}
	if err := WriteBlock(db, block); err != nil {
		return nil, err
	}
	if err := WriteBlockReceipts(db, block.Hash(), block.NumberU64(), nil); err != nil {
		return nil, err
	}
	if err := WriteCanonicalHash(db, block.Hash(), block.NumberU64()); err != nil {
		return nil, err
	}
	if err := WriteHeadBlockHash(db, block.Hash()); err != nil {
		return nil, err
	}
	if err := WriteHeadHeaderHash(db, block.Hash()); err != nil {
		return nil, err
	}
	configtemp := g.Config
	if configtemp == nil {
		configtemp = config.MainnetChainConfig
	}

	return block, WriteChainConfig(db, block.Hash(), configtemp)
}

// MustCommit writes the genesis block and state to db, panicking on error.
// The block is committed as the canonical head block.
func (g *Genesis) MustCommit(db shxdb.Database) *types.Block {
	block, err := g.Commit(db)
	if err != nil {
		panic(err)
	}
	return block
}

// GenesisBlockForTesting creates and writes a block in which addr has the given wei balance.
func GenesisBlockForTesting(db shxdb.Database, addr common.Address, balance *big.Int) *types.Block {
	g := Genesis{}
	return g.MustCommit(db)
}

// DefaultGenesisBlock returns the Shx main net genesis block.
func DefaultGenesisBlock() *Genesis {
	return &Genesis{
		Config:     config.MainnetChainConfig,
		ExtraData:  hexutil.MustDecode("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa"),
		Difficulty: big.NewInt(17179869184),
	}
}

// DefaultTestnetGenesisBlock returns the Ropsten network genesis block.
func DefaultTestnetGenesisBlock() *Genesis {
	return &Genesis{
		Config:     config.MainnetChainConfig,
		ExtraData:  hexutil.MustDecode("0x3535353535353535353535353535353535353535353535353535353535353535"),
		Difficulty: big.NewInt(1048576),
	}
}
