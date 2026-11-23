package liveops

import (
	"errors"
	"slices"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
)

// LedgerLimit is how many transaction IDs each list of a Ledger keeps; the oldest are dropped past it. A store hands a game only its unfinished transactions again, and a finished transaction is never far back in the list, so the limit only bounds the save's size.
const LedgerLimit = 1024

// ErrNoLedger is what Buy and every delivery answer when PurchasesOptions has no Ledger to record credits in.
var ErrNoLedger = errors.New("liveops: purchases need a ledger")

// Ledger records the transactions a game has credited and revoked, so each is applied once across runs. Keep it inside the state PurchasesOptions.Save writes, beside the coins, lives or other state a credit changes: the credit and its record then reach the disk in one write, so a kill before that write loses both and the store's redelivery credits it again, and a kill after it finds the record and only finishes the transaction.
type Ledger struct {
	// Credited are the IDs of consumable transactions credited, oldest first.
	Credited []string `json:"credited,omitempty"`
	// Revoked are the IDs of credited consumable transactions whose refund or revocation was taken back, oldest first.
	Revoked []string `json:"revoked,omitempty"`
}

// HasCredited reports whether the transaction whose ID is id was credited.
func (l Ledger) HasCredited(id string) bool { return slices.Contains(l.Credited, id) }

// HasRevoked reports whether the refund or revocation of the transaction whose ID is id was taken back.
func (l Ledger) HasRevoked(id string) bool { return slices.Contains(l.Revoked, id) }

// record appends id to list, dropping the oldest IDs past LedgerLimit.
func record(list []string, id string) []string {
	list = append(list, id)
	if n := len(list) - LedgerLimit; n > 0 {
		list = slices.Delete(list, 0, n)
	}
	return list
}

// BuyResult is how a Buy ended without an error.
type BuyResult int

const (
	// BuyFailed is the result that goes with an error.
	BuyFailed BuyResult = iota
	// Bought: the purchase went through and was delivered, its credit saved or its unlock owned.
	Bought
	// BuyPending: the purchase waits for approval, as Ask to Buy's does; Pending reports it until its transaction arrives through the store's updates, which deliver it as Buy would have.
	BuyPending
	// BuyCancelled: the user closed the store's sheet.
	BuyCancelled
)

// String returns "failed", "bought", "pending" or "cancelled".
func (r BuyResult) String() string {
	switch r {
	case Bought:
		return "bought"
	case BuyPending:
		return "pending"
	case BuyCancelled:
		return "cancelled"
	default:
		return "failed"
	}
}

// PurchasesOptions configures NewPurchases.
type PurchasesOptions struct {
	// Ledger is the game's record of credited transactions, which Save must persist along with the state Credit and Revoke change. It is required.
	Ledger *Ledger
	// Credit applies a consumable transaction to the game's state, tr.Quantity of tr.ProductID. It runs at most once per transaction ID, across runs too, as long as Save persists Ledger with that state.
	Credit func(tr core.Transaction)
	// Revoke takes back what a credited consumable gave when the store refunds or revokes it; nil keeps the credit. It runs at most once per transaction ID, as Credit does.
	Revoke func(tr core.Transaction)
	// Save persists the game's save, Ledger included, after Credit or Revoke changed it. A transaction is finished only once Save returns nil: after an error it stays unfinished, the store hands it to the app again at its next launch, and the ledger kept in memory or on disk keeps Credit from running twice. It is required for consumables.
	Save func() error
	// Verify hands tr, whose Signed is the store's JWS, to the game's server and calls done once with nil to accept it or an error to refuse it, from any goroutine. A refused transaction is neither delivered nor finished. nil accepts what the device verified, Transaction.Verified.
	Verify func(tr core.Transaction, done func(error))
	// AccountToken is the UUID of the game's own that each purchase carries to the store and the game's server, "" for none.
	AccountToken string
	// OnError hears failures that no Buy or Restore answers: a transaction from the store's updates refused, unsaved or not finished, or entitlements the store could not answer.
	OnError func(tr core.Transaction, err error)
}

// Purchases sells a game's in-app products over NSGo's purchase services: it credits consumables into the save exactly once, keeps unlocks and subscriptions from the store's entitlements, restores them, follows purchases waiting for approval and hands each transaction to an optional server before delivering it. Its methods run on the scope's goroutine, as NSGo's callbacks do.
type Purchases struct {
	cx    *core.Scope
	opts  PurchasesOptions
	rev   *core.Signal[int]
	owned map[string]core.Transaction
	// pending are the products whose purchase waits for approval.
	pending map[string]bool
	// inflight are the transactions being verified, saved and finished, with what waits for them.
	inflight map[string][]func(error)
}

// NewPurchases starts selling with opts under cx: it registers for the store's transaction updates, which hand over unfinished transactions as the app starts, and reads the user's current entitlements. Call it as the app starts, before the first frame, so no update waits.
func NewPurchases(cx *core.Scope, opts PurchasesOptions) *Purchases {
	p := &Purchases{
		cx:       cx,
		opts:     opts,
		rev:      core.NewSignal(cx, 0),
		owned:    map[string]core.Transaction{},
		pending:  map[string]bool{},
		inflight: map[string][]func(error){},
	}
	cx.OnPurchaseUpdate(func(tr core.Transaction) {
		p.handle(tr, func(err error) {
			if err != nil {
				p.report(tr, err)
			}
		})
	})
	if core.Untracked(cx, cx.Env().CanPurchase) {
		p.Refresh(nil)
	}
	return p
}

// Available reports whether this platform and build reach a store, core.Env.CanPurchase; read in a reactive computation, it follows it.
func (p *Purchases) Available() bool { return p.cx.Env().CanPurchase() }

// Owned reports whether the user owns product now: a non-consumable or a subscription that is neither refunded nor past its expiration date at the scope's wall time. Read in a reactive computation, it follows purchases, refunds and restores.
func (p *Purchases) Owned(product string) bool {
	p.rev.Get()
	tr, ok := p.owned[product]
	if !ok {
		return false
	}
	return tr.ExpirationDate.IsZero() || p.cx.WallTime().Before(tr.ExpirationDate)
}

// Entitlement returns the latest transaction that gives the user product, and whether there is one, whatever its expiration date.
func (p *Purchases) Entitlement(product string) (core.Transaction, bool) {
	p.rev.Get()
	tr, ok := p.owned[product]
	return tr, ok
}

// Pending reports whether a purchase of product waits for approval. Read in a reactive computation, it follows the approval.
func (p *Purchases) Pending(product string) bool {
	p.rev.Get()
	return p.pending[product]
}

// Buy buys quantity of product (0 is 1) through the store's sheet and answers done once: Bought after the transaction is verified and delivered, BuyPending or BuyCancelled, or BuyFailed with the error. done may be nil.
func (p *Purchases) Buy(product string, quantity int, done func(BuyResult, error)) {
	answer := func(r BuyResult, err error) {
		if done != nil {
			done(r, err)
		}
	}
	if p.opts.Ledger == nil {
		answer(BuyFailed, ErrNoLedger)
		return
	}
	p.cx.Purchase(product, core.PurchaseOptions{AccountToken: p.opts.AccountToken, Quantity: quantity}, func(tr core.Transaction, err error) {
		switch {
		case errors.Is(err, core.PurchaseCancelled):
			answer(BuyCancelled, nil)
		case errors.Is(err, core.PurchasePending):
			p.pending[product] = true
			p.changed()
			answer(BuyPending, nil)
		case err != nil:
			answer(BuyFailed, err)
		default:
			p.handle(tr, func(err error) {
				if err != nil {
					answer(BuyFailed, err)
					return
				}
				answer(Bought, nil)
			})
		}
	})
}

// Refresh reads the user's current entitlements from the store and makes them what Owned reports, then answers done, which may be nil. NewPurchases calls it; call it again as the app returns to the foreground. An error goes to done, or to OnError when done is nil.
func (p *Purchases) Refresh(done func(error)) {
	p.cx.CurrentEntitlements(func(txs []core.Transaction, err error) {
		if err == nil {
			owned := make(map[string]core.Transaction, len(txs))
			for _, tr := range txs {
				if tr.ProductType != core.ProductConsumable && tr.RevocationDate.IsZero() {
					owned[tr.ProductID] = tr
				}
			}
			p.owned = owned
			p.changed()
		}
		switch {
		case done != nil:
			done(err)
		case err != nil:
			p.report(core.Transaction{}, err)
		}
	})
}

// Restore has the store bring the user's purchases up to date, which may ask them to sign in, then refreshes what Owned reports, and answers done, which may be nil: nil, or the store's error, core.PurchaseCancelled when the user declined to sign in. Call it only from a Restore Purchases button the user presses.
func (p *Purchases) Restore(done func(error)) {
	answer := func(err error) {
		if done != nil {
			done(err)
		}
	}
	p.cx.SyncPurchases(func(err error) {
		if err != nil {
			answer(err)
			return
		}
		p.Refresh(answer)
	})
}

// handle verifies, delivers and finishes tr, then calls done with why it was not delivered, or nil. A transaction already in flight waits for that run instead of starting another.
func (p *Purchases) handle(tr core.Transaction, done func(error)) {
	if waiting, ok := p.inflight[tr.ID]; ok {
		p.inflight[tr.ID] = append(waiting, done)
		return
	}
	p.inflight[tr.ID] = []func(error){done}
	p.verify(tr, func(err error) {
		if err == nil {
			err = p.deliver(tr)
		}
		if err == nil {
			p.cx.FinishTransaction(tr.ID, func(err error) {
				// An unknown ID was finished already, which is what was asked.
				if err != nil && !errors.Is(err, core.PurchaseNotFound) {
					p.report(tr, err)
				}
			})
		}
		waiting := p.inflight[tr.ID]
		delete(p.inflight, tr.ID)
		p.changed()
		for _, w := range waiting {
			w(err)
		}
	})
}

// verify runs Verify on tr and calls next once on the scope's goroutine, however Verify answers.
func (p *Purchases) verify(tr core.Transaction, next func(error)) {
	if p.opts.Verify == nil {
		if !tr.Verified {
			next(errors.New("liveops: the device could not verify transaction " + tr.ID))
			return
		}
		next(nil)
		return
	}
	var answered atomic.Bool
	p.opts.Verify(tr, func(err error) {
		if answered.Swap(true) {
			return
		}
		p.cx.Post(func() { next(err) })
	})
}

// deliver applies tr to the game: a consumable's credit or its revocation into the save, once each, or an unlock's ownership.
func (p *Purchases) deliver(tr core.Transaction) error {
	l := p.opts.Ledger
	if l == nil {
		return ErrNoLedger
	}
	delete(p.pending, tr.ProductID)
	if tr.ProductType != core.ProductConsumable {
		if tr.RevocationDate.IsZero() {
			p.owned[tr.ProductID] = tr
		} else if o, ok := p.owned[tr.ProductID]; ok && (o.ID == tr.ID || o.OriginalID == tr.OriginalID) {
			delete(p.owned, tr.ProductID)
		}
		return nil
	}
	switch {
	case tr.RevocationDate.IsZero() && !l.HasCredited(tr.ID):
		if p.opts.Credit == nil || p.opts.Save == nil {
			return errors.New("liveops: consumable " + tr.ProductID + " needs Credit and Save")
		}
		p.opts.Credit(tr)
		l.Credited = record(l.Credited, tr.ID)
	case !tr.RevocationDate.IsZero() && l.HasCredited(tr.ID) && !l.HasRevoked(tr.ID) && p.opts.Revoke != nil:
		if p.opts.Save == nil {
			return errors.New("liveops: consumable " + tr.ProductID + " needs Save")
		}
		p.opts.Revoke(tr)
		l.Revoked = record(l.Revoked, tr.ID)
	default:
		// Credited or taken back already, maybe saved by a run killed before it finished the transaction, or a refund of one never credited.
		if p.opts.Save == nil {
			return nil
		}
	}
	// Saving again after an earlier run's failed save is what makes its credit durable before the finish.
	return p.opts.Save()
}

func (p *Purchases) changed() { p.rev.Update(func(n int) int { return n + 1 }) }

func (p *Purchases) report(tr core.Transaction, err error) {
	if p.opts.OnError != nil {
		p.opts.OnError(tr, err)
	}
}
