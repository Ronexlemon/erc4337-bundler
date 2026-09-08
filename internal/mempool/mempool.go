package mempool

import (
	"erc4337-bundler/pkg/types"
	"sync"
)

type poolKey struct{
	sender string
	nonce string
}

type Mempool struct{
	mu  sync.Mutex
	ops  map[poolKey]types.UserOperation
}

func NewMemPool()*Mempool{
	return &Mempool{ops: make(map[poolKey]types.UserOperation)}
}


// Add inserts or replaces a UserOp, Returns false if same-or-higer-fee op already exists

func (m *Mempool) Add(op types.UserOperation)bool{
	m.mu.Lock()
	defer m.mu.Unlock()

	key := poolKey{sender: op.Sender.Hex(),nonce: op.Nonce.String()}

	if existing, ok := m.ops[key];ok{
		// require a meaniful fee bump to replace, mirros geth's tx-pool rule
		if op.MaxPriorityFeePerGas.Cmp(existing.MaxPriorityFeePerGas) <=0{
			return false
		}
	}
	m.ops[key] =op
	return true
}

func (m *Mempool) All()[]types.UserOperation{
	m.mu.Lock()
	defer m.mu.Unlock()
	out :=make([]types.UserOperation,0,len(m.ops))

	for _,op :=range m.ops{
		out =append(out, op)
	}
	return out
}

func (m *Mempool)Remove(sender,nonce string){
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ops,poolKey{sender: sender,nonce: nonce})
}