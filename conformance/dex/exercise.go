// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// The script. One list of operations, sent to every implementation in the same
// order by the same three keys, with the contract's state read back after each
// one.
//
// The state is read through eth_getStorageAt and nothing else. That is not a
// stylistic choice: of the three implementations, one serves no eth_call, no
// receipts and no logs, so a harness built on view functions would silently
// become a two-implementation harness. Storage is the widest surface all three
// agree to answer on, and it is also the surface consensus is actually about.

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/luxfi/crypto"
	"github.com/luxfi/geth/common"
)

//go:embed Dex.bin
var dexHex string

// The traders. Three, because two cannot tell a self-trade from a crossing
// trade: with two parties every cross is somebody's counterparty, and the
// refusal being tested would never be reached.
const (
	maker = 0 // rests the book
	rival = 1 // crosses it
	third = 2 // the party that neither of the other two is
)

// Traders is the SAME three addresses, in the same roles, on every chain.
//
// This is load-bearing, and getting it wrong produced a false disagreement the
// first time this harness ran. Both comparable digests fold `msg.sender` into
// themselves — they must, since who owns a resting order is part of the book.
// So if each chain let its own genesis decide which key played maker, the three
// would fold three different addresses and report a disagreement about
// matching order that was really a disagreement about whose money it was. The
// addresses are pinned here; which account PAYS is a separate question,
// answered per chain by whatever that genesis happened to fund.
func Traders() []Account {
	out := make([]Account, 0, 3)
	if a, err := FromHex(AnvilKey, "anvil/0"); err == nil {
		out = append(out, a)
	}
	for i := uint32(0); i < 2; i++ {
		if a, err := BIP44(Mnemonic, 60, i); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// Slots 0..9 are the contract's whole comparable state. The names are the
// declaration order in Dex.sol; Solidity lays public value types out in that
// order, and the harness asserts it by reading a value it set.
var slotNames = []string{
	"poolBase", "poolQuote", "poolShares",
	"nextId", "fillCount", "fills", "book", "liveOrders", "refusals", "tradedQty",
}

// Step is one operation and what the chain did with it.
type Step struct {
	Name    string
	Sender  int
	Expect  string // "apply" or "refuse"
	Sent    bool
	Mined   bool // the nonce advanced, so the chain processed it
	Changed bool // the contract's state moved
	Digest  string
	Err     string
}

// Run is one implementation's whole answer.
type Run struct {
	Node     *Node
	Ready    bool
	Why      string
	Contract common.Address
	Steps    []Step
	Slots    map[string]string
	Digest   string
	Root     string // the block's state root, for the record
	Height   string
}

type driver struct {
	n      *Node
	who    []Account
	nonce  map[common.Address]uint64
	at     common.Address
}

// nonceOf reads the chain's accepted nonce once per account and counts from
// there. Keying by address rather than by position matters because the payer is
// often also a trader, and two counters for one account would collide.
func (d *driver) nonceOf(a Account) (uint64, error) {
	if n, ok := d.nonce[a.Addr]; ok {
		return n, nil
	}
	n, err := d.n.Nonce(a.Addr)
	if err != nil {
		return 0, err
	}
	d.nonce[a.Addr] = n
	return n, nil
}

// send signs one call, submits it, and waits until the sender's nonce has
// advanced. Waiting on the nonce rather than on a receipt is what makes the
// same code work against all three: a nonce that moved proves the transaction
// was executed, and a transaction that was executed and reverted is exactly
// what a refusal looks like.
func (d *driver) sendAs(a Account, to *common.Address, value *big.Int, data []byte, gas uint64) (bool, error) {
	non, err := d.nonceOf(a)
	if err != nil {
		return false, err
	}
	raw, err := SignNonce(a, d.n, non, to, value, data, gas)
	if err != nil {
		return false, err
	}
	if _, err := d.n.SendRaw(raw); err != nil {
		return false, err
	}
	deadline := time.Now().Add(sendWait)
	for time.Now().Before(deadline) {
		got, err := d.n.Nonce(a.Addr)
		if err == nil && got > non {
			d.nonce[a.Addr] = got
			return true, nil
		}
		time.Sleep(400 * time.Millisecond)
	}
	// Give up on this operation, but resync from the chain first. A transaction
	// that was slow rather than refused lands after the wait, and a harness
	// that kept its own count would then send every later transaction at a
	// nonce the chain has already used — turning one slow block into a whole
	// account that never speaks again, and reporting it as the chain's fault.
	if got, err := d.n.Nonce(a.Addr); err == nil {
		d.nonce[a.Addr] = got
	}
	return false, fmt.Errorf("submitted but not executed within %s", sendWait)
}

func (d *driver) send(i int, to *common.Address, data []byte, gas uint64) (bool, error) {
	return d.sendAs(d.who[i], to, big.NewInt(0), data, gas)
}

// sendWait is generous because one implementation charges a block cost that a
// quiet chain pays for with time.
const sendWait = 150 * time.Second

// digest folds slots 0..9 into one word. Two implementations that ran the same
// script agree on it or they do not; there is no third answer.
func (d *driver) digest() (string, map[string]string, error) {
	out := map[string]string{}
	h := crypto.Keccak256Hash()
	for i, name := range slotNames {
		v, err := d.n.StorageAt(d.at, common.BigToHash(big.NewInt(int64(i))))
		if err != nil {
			return "", nil, err
		}
		out[name] = v.Hex()
		h = crypto.Keccak256Hash(h.Bytes(), v.Bytes())
	}
	return h.Hex(), out, nil
}

// Exercise deploys the market and walks the script.
func Exercise(n *Node, p *Probe, permute bool) *Run {
	r := &Run{Node: n}
	if p.ChainID == nil {
		r.Why = "no chain: " + p.Note
		return r
	}
	n.ChainID = p.ChainID
	if p.Funded == nil {
		r.Why = "no key holds a balance on this chain, so nothing can be sent"
		return r
	}
	if !p.Includes {
		r.Why = "the chain does not execute a submitted transaction: " + p.Note
		return r
	}

	code, err := hex.DecodeString(strings.TrimSpace(dexHex))
	if err != nil {
		r.Why = "bad embedded bytecode: " + err.Error()
		return r
	}

	// The same three addresses in the same roles everywhere; the payer is
	// whichever of this chain's keys holds money.
	who := Traders()
	if len(who) != 3 {
		r.Why = "could not derive the three trader keys"
		return r
	}
	payer := *p.Funded
	d := &driver{n: n, who: who, nonce: map[common.Address]uint64{}}

	// Gas money for any trader this chain did not fund. A plain transfer is the
	// one operation every implementation certainly supports, so the setup
	// cannot be what fails. Five ether covers every step at this harness's
	// deliberately generous gas price.
	need := new(big.Int).Mul(big.NewInt(5), big.NewInt(1e18))
	topped := []common.Address{}
	for _, t := range who {
		if t.Addr == payer.Addr {
			continue
		}
		bal, err := n.Balance(t.Addr)
		if err != nil {
			r.Why = "balance: " + err.Error()
			return r
		}
		if bal.Cmp(need) >= 0 {
			continue
		}
		if _, err := d.sendAs(payer, &t.Addr, need, nil, 21000); err != nil {
			r.Why = "funding " + t.Addr.Hex() + ": " + err.Error()
			return r
		}
		topped = append(topped, t.Addr)
	}
	for _, a := range topped {
		if b, err := n.Balance(a); err != nil || b.Sign() == 0 {
			r.Why = "funded " + a.Hex() + " but the balance never arrived"
			return r
		}
	}

	// Deploy. The address is the CREATE rule rather than a receipt field,
	// because one of the three serves no receipts.
	non, err := d.nonceOf(payer)
	if err != nil {
		r.Why = "nonce: " + err.Error()
		return r
	}
	d.at = common.Address(crypto.CreateAddress(
		crypto.PubkeyToAddress(payer.Priv.PublicKey), non))
	ok, err := d.sendAs(payer, nil, big.NewInt(0), deployData(code, []common.Address{
		who[0].Addr, who[1].Addr, who[2].Addr}, 1_000_000, 2_000_000), 3_000_000)
	if err != nil || !ok {
		r.Why = "deploy: " + errText(err)
		return r
	}
	live, err := n.Code(d.at)
	if err != nil || len(live) == 0 {
		r.Why = fmt.Sprintf("deployed to %s but no code is there", d.at.Hex())
		return r
	}
	r.Contract = d.at
	r.Ready = true

	for _, s := range script(permute) {
		st := Step{Name: s.name, Sender: s.sender, Expect: s.expect}
		before, _, derr := d.digest()
		if derr != nil {
			st.Err = "read: " + derr.Error()
			r.Steps = append(r.Steps, st)
			continue
		}
		mined, err := d.send(s.sender, &d.at, s.data, s.gas)
		st.Sent = true
		st.Mined = mined
		if err != nil {
			st.Err = err.Error()
		}
		after, _, derr := d.digest()
		if derr != nil {
			st.Err = "read: " + derr.Error()
		}
		st.Changed = after != before
		st.Digest = after
		r.Steps = append(r.Steps, st)
	}

	r.Digest, r.Slots, _ = d.digest()
	if root, h, err := n.StateRoot(); err == nil {
		r.Root, r.Height = root.Hex(), h
	}
	return r
}

func errText(e error) string {
	if e == nil {
		return "the nonce never advanced"
	}
	return e.Error()
}

type op struct {
	name   string
	sender int
	expect string
	gas    uint64
	data   []byte
}

// script is the whole exercise, in the order every implementation runs it.
//
// The book is built so that matching order is decidable and observable: two
// asks rest at the same price so only arrival separates them, and a third rests
// better, so a correct sweep takes price first and arrival second. A node that
// took them in any other order folds a different `fills`.
//
// `permute` swaps the arrival of the two same-price asks and changes NOTHING
// else. It is the harness's own control: the same quantity trades at the same
// prices, so every count and every amount must come out identical, and only the
// SEQUENCE of makers differs. If `fills` did not move under it, `fills` would
// not be measuring matching order, and three implementations agreeing on it
// would mean nothing at all.
func script(permute bool) []op {
	addLiq := func(b, q uint64) []byte {
		return call("addLiquidity(uint128,uint128)", num(b), num(q))
	}
	swap := func(buyBase bool, in uint64) []byte {
		return call("swap(bool,uint128)", boolWord(buyBase), num(in))
	}
	rmLiq := func(n uint64) []byte { return call("removeLiquidity(uint256)", num(n)) }
	place := func(buy bool, price, qty uint64) []byte {
		return call("place(bool,uint96,uint96)", boolWord(buy), num(price), num(qty))
	}
	cancel := func(id uint64) []byte { return call("cancel(uint256)", num(id)) }

	const G = 1_500_000

	// The two asks that share a price. Only their arrival separates them.
	first := op{"book/ask 100@20 maker", maker, "apply", G, place(false, 20, 100)}
	second := op{"book/ask 200@20 rival (same price, later)", rival, "apply", G, place(false, 20, 200)}
	if permute {
		first = op{"book/ask 200@20 rival", rival, "apply", G, place(false, 20, 200)}
		second = op{"book/ask 100@20 maker (same price, later)", maker, "apply", G, place(false, 20, 100)}
	}

	return []op{
		// -- the pool -----------------------------------------------------
		{"amm/addLiquidity maker", maker, "apply", G, addLiq(100_000, 200_000)},
		{"amm/addLiquidity rival", rival, "apply", G, addLiq(50_000, 100_000)},
		{"amm/swap quote->base", third, "apply", G, swap(true, 10_000)},
		{"amm/swap base->quote", maker, "apply", G, swap(false, 5_000)},
		{"amm/removeLiquidity rival", rival, "apply", G, rmLiq(50_000)},
		{"amm/swap unfunded (refused)", third, "refuse", G, swap(true, 90_000_000)},

		// -- the book -----------------------------------------------------
		first,
		second,
		{"book/ask 50@19 maker (better price)", maker, "apply", G, place(false, 19, 50)},
		{"book/bid 100@18 maker (rests, no cross)", maker, "apply", G, place(true, 18, 100)},
		{"book/cancel the bid", maker, "apply", G, cancel(3)},

		// The crossing order. At limit 20 it must take 19 before 20, and among
		// the two at 20 the one that arrived first.
		{"book/bid 120@20 third (crosses two levels)", third, "apply", G, place(true, 20, 120)},

		// A self-trade, made unambiguous: rival's own ask is the best in the
		// book when rival crosses it.
		{"book/ask 10@15 rival (best in book)", rival, "apply", G, place(false, 15, 10)},
		{"book/bid 10@16 rival (self-trade, refused)", rival, "refuse", G, place(true, 16, 10)},

		// An order nobody can pay for.
		{"book/bid 60000@60000 third (unfunded, refused)", third, "refuse", G, place(true, 60_000, 60_000)},

		// The book still works after the refusals.
		{"book/bid 10@16 third (takes rival's ask)", third, "apply", G, place(true, 16, 10)},
	}
}
