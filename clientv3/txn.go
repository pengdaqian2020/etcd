// Copyright 2016 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package clientv3

import (
	"sync"

	"github.com/coreos/etcd/Godeps/_workspace/src/golang.org/x/net/context"
	pb "github.com/coreos/etcd/etcdserver/etcdserverpb"
)

//
// Tx.If(
//  Compare(Value(k1), ">", v1),
//  Compare(Version(k1), "=", 2)
// ).Then(
//  OpPut(k2,v2), OpPut(k3,v3)
// ).Else(
//  OpPut(k4,v4), OpPut(k5,v5)
// ).Commit()
type Txn interface {
	// If takes a list of comparison. If all comparisons passed in succeed,
	// the operations passed into Then() will be executed. Or the operations
	// passed into Else() will be executed.
	If(cs ...Cmp) Txn

	// Then takes a list of operations. The Ops list will be executed, if the
	// comparisons passed in If() succeed.
	Then(ops ...Op) Txn

	// Else takes a list of operations. The Ops list will be executed, if the
	// comparisons passed in If() fail.
	Else(ops ...Op) Txn

	// Commit tries to commit the transaction.
	Commit() (*TxnResponse, error)

	// TODO: add a Do for shortcut the txn without any condition?
}

type txn struct {
	kv *kv

	mu    sync.Mutex
	cif   bool
	cthen bool
	celse bool

	cmps []*pb.Compare

	sus []*pb.RequestUnion
	fas []*pb.RequestUnion
}

func (txn *txn) If(cs ...Cmp) *txn {
	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.cif {
		panic("cannot call If twice!")
	}

	if txn.cthen {
		panic("cannot call If after Then!")
	}

	if txn.celse {
		panic("cannot call If after Else!")
	}

	for _, cmp := range cs {
		txn.cmps = append(txn.cmps, (*pb.Compare)(&cmp))
	}

	return txn
}

func (txn *txn) Then(ops ...Op) *txn {
	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.cthen {
		panic("cannot call Then twice!")
	}
	if txn.celse {
		panic("cannot call Then after Else!")
	}

	txn.cthen = true

	for _, op := range ops {
		txn.sus = append(txn.sus, op.toRequestUnion())
	}

	return txn
}

func (txn *txn) Else(ops ...Op) *txn {
	txn.mu.Lock()
	defer txn.mu.Unlock()

	if txn.celse {
		panic("cannot call Else twice!")
	}

	txn.celse = true

	for _, op := range ops {
		txn.fas = append(txn.fas, op.toRequestUnion())
	}

	return txn
}

func (txn *txn) Commit() (*TxnResponse, error) {
	kv := txn.kv
	for {
		r := &pb.TxnRequest{Compare: txn.cmps, Success: txn.sus, Failure: txn.fas}
		resp, err := kv.remote.Txn(context.TODO(), r)
		if err == nil {
			return (*TxnResponse)(resp), nil
		}

		// TODO: this can cause data race with other kv operation.
		newConn, cerr := kv.c.retryConnection(kv.conn, err)
		if cerr != nil {
			// TODO: return client lib defined connection error
			return nil, cerr
		}
		kv.conn = newConn
		kv.remote = pb.NewKVClient(kv.conn)
	}
}
