package main

import (
	"bytes"
	"crypto/rsa"
	"sync"
)

type State struct {
	sync.RWMutex

	// main state
	primary    *BlockChain
	alternates []*BlockChain
	wallet     *Wallet
	keys       KeySet

	pendingTxns    []*Transaction
	ResetMiner     bool
}

func NewState() *State {
	s := &State{}
	s.primary = &BlockChain{}
	s.wallet = NewWallet()

	return s
}

//
// public, locked functions
//

func (s *State) GetWallet() map[rsa.PublicKey]uint64 {
	s.RLock()
	defer s.RUnlock()

	ret := make(map[rsa.PublicKey]uint64)

	for key, _ := range s.wallet.Keys {
		txn := s.primary.ActiveKeys[key]

		if txn == nil {
			ret[key] = 0
		} else {
			_, ret[key] = txn.OutputAmount(key)
		}
	}

	return ret
}

func (s *State) ConstructBlock() *Block {
	s.Lock()
	defer s.Unlock()

	b := &Block{}
	b.Txns = append(b.Txns, NewMinersTransation(s.wallet))
	b.Txns = append(b.Txns, s.pendingTxns...)

	if s.primary.Last() != nil {
		b.PrevHash = s.primary.Last().Hash()
	}
	s.ResetMiner = false

	return b
}

func (s *State) ChainFromHash(hash []byte) *BlockChain {
	s.RLock()
	defer s.RUnlock()
	return s.chainFromHash(hash)
}

func (s *State) AddBlockChain(chain *BlockChain) {
	s.Lock()
	defer s.Unlock()

	if len(s.primary.Blocks) < len(chain.Blocks) {
		s.alternates = append(s.alternates, s.primary)
		s.primary = chain
		s.reset()
	}
}

// first return is if the block was accepted, second
// is if we already have the relevant chain
func (s *State) NewBlock(b *Block) (bool, bool) {
	if !b.Verify() {
		return false, false
	}

	s.Lock()
	defer s.Unlock()

	chain := s.chainFromHash(b.PrevHash)

	if chain == nil {
		return true, false
	}

	success := chain.Append(b)
	if !success {
		return false, true
	}

	s.reset()

	return true, true
}

func (s *State) UndoBlock(b *Block) {
	s.Lock()
	defer s.Unlock()

	lastTxn := b.Txns[len(b.Txns)-1]
	if lastTxn.Inputs == nil && len(lastTxn.Outputs) == 1 {
		// miner's transaction, remove the key
		delete(s.wallet.Keys, lastTxn.Outputs[0].Key)
	}
}

//
// private, unlocked functions *must* be called while already holding the lock
//

func (s *State) reset() {
	s.ResetMiner = true
	s.keys = s.primary.ActiveKeys.Copy()

	var tmp []*Transaction

	for _, txn := range s.pendingTxns {
		if s.keys.AddTxn(txn) {
			tmp = append(tmp, txn)
		}
	}

	s.pendingTxns = tmp
}

func (s *State) chainFromHash(hash []byte) *BlockChain {
	if hash == nil {
		return s.primary
	}

	if bytes.Equal(hash, s.primary.Last().Hash()) {
		return s.primary
	}

	for _, chain := range s.alternates {
		if bytes.Equal(hash, chain.Last().Hash()) {
			return chain
		}
	}

	return nil
}
