package liveops_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/liveops"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var storeKit = nsgotest.Environment(core.Environment{Purchases: true})

var (
	coins   = core.Product{ID: "coins.100", Type: core.ProductConsumable, Title: "100 coins", DisplayPrice: "$0.99"}
	noAds   = core.Product{ID: "unlock.noads", Type: core.ProductNonConsumable, Title: "No ads", DisplayPrice: "$2.99"}
	monthly = core.Product{ID: "pass.monthly", Type: core.ProductAutoRenewable, Title: "Season pass", DisplayPrice: "$4.99",
		Subscription: &core.Subscription{GroupID: "pass", Period: core.SubscriptionPeriod{Unit: core.PeriodMonth, Value: 1}}}
)

// progress is a game's save: the coins purchases credit and the ledger that records them, written together.
type progress struct {
	Coins     int            `json:"coins"`
	Purchases liveops.Ledger `json:"purchases"`
}

// shop is a game run: its save in the app's storage, its purchases and what they reported.
type shop struct {
	app       *nsgotest.App
	cx        *core.Scope
	state     progress
	saves     *save.Store
	purchases *liveops.Purchases
	credits   int
	errs      []error
	// saveErr fails the next saves; afterSave runs after each successful one.
	saveErr   error
	afterSave func()
	verify    func(tr core.Transaction, done func(error))
}

// open runs the game as it starts: it loads the save from storage, then starts selling.
func open(t *testing.T, opts ...nsgotest.Option) *shop {
	t.Helper()
	s := &shop{}
	s.app = nsgotest.StartApp(t, func(cx *core.Scope) {
		s.cx = cx
		s.saves = save.NewStorageStore(cx.Storage(), "game")
		if err := s.saves.Load("main", &s.state); err != nil && !errors.Is(err, save.ErrNotFound) {
			t.Fatal(err)
		}
		s.purchases = liveops.NewPurchases(cx, liveops.PurchasesOptions{
			Ledger: &s.state.Purchases,
			Credit: func(tr core.Transaction) {
				s.credits++
				s.state.Coins += 100 * tr.Quantity
			},
			Revoke: func(tr core.Transaction) { s.state.Coins -= 100 * tr.Quantity },
			Save: func() error {
				if s.saveErr != nil {
					return s.saveErr
				}
				if err := s.saves.Save("main", &s.state); err != nil {
					return err
				}
				if s.afterSave != nil {
					s.afterSave()
				}
				return nil
			},
			Verify: func(tr core.Transaction, done func(error)) {
				if s.verify != nil {
					s.verify(tr, done)
					return
				}
				done(nil)
			},
			OnError: func(_ core.Transaction, err error) { s.errs = append(s.errs, err) },
		})
	}, append([]nsgotest.Option{storeKit}, opts...)...)
	s.app.Products = []core.Product{coins, noAds, monthly}
	return s
}

// disk is what the app's storage holds now, as a later run starts with it.
func (s *shop) disk() map[string]string {
	st := s.cx.Storage()
	out := map[string]string{}
	for _, k := range st.Keys() {
		out[k], _ = st.GetItem(k)
	}
	return out
}

// saved is the progress the storage holds.
func (s *shop) saved(t *testing.T) progress {
	t.Helper()
	var p progress
	if err := s.saves.Load("main", &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (s *shop) buy(product string, quantity int) (liveops.BuyResult, error) {
	var res liveops.BuyResult = -1
	var err error
	s.purchases.Buy(product, quantity, func(r liveops.BuyResult, e error) { res, err = r, e })
	s.app.Settle()
	return res, err
}

func TestBuyCreditsAConsumableIntoTheSaveAndFinishesIt(t *testing.T) {
	s := open(t)
	res, err := s.buy("coins.100", 2)
	if res != liveops.Bought || err != nil {
		t.Fatalf("buy: %v, %v", res, err)
	}
	if s.state.Coins != 200 || s.credits != 1 {
		t.Fatalf("coins %d after %d credits, want 200 after 1", s.state.Coins, s.credits)
	}
	if len(s.app.Unfinished) != 0 || len(s.app.Finished) != 1 {
		t.Fatalf("unfinished %v, finished %v", s.app.Unfinished, s.app.Finished)
	}
	got := s.saved(t)
	if got.Coins != 200 || !slices.Equal(got.Purchases.Credited, s.app.Finished) {
		t.Fatalf("saved %+v, finished %v", got, s.app.Finished)
	}
	r := s.app.PurchaseRequests
	if i := slices.IndexFunc(r, func(r core.PurchaseRequest) bool { return r.Command == core.PurchaseBuy }); i < 0 || r[i].Quantity != 2 {
		t.Fatalf("requests %+v", r)
	}
}

// The app is killed after the credit reached the save but before the store heard the transaction finished: the next run is handed it again and only finishes it.
func TestAConsumableKilledBeforeFinishingIsCreditedOnce(t *testing.T) {
	first := open(t)
	var disk map[string]string
	var unfinished []core.Transaction
	first.afterSave = func() { disk, unfinished = first.disk(), slices.Clone(first.app.Unfinished) }
	if res, err := first.buy("coins.100", 1); res != liveops.Bought || err != nil {
		t.Fatalf("buy: %v, %v", res, err)
	}
	// The app dies right after the save: the store still holds the transaction unfinished.
	if disk == nil || len(unfinished) != 1 {
		t.Fatalf("at the save: disk %v, unfinished %v", disk, unfinished)
	}
	tr := unfinished[0].ID

	second := open(t, nsgotest.Storage(disk), nsgotest.UnfinishedTransactions(unfinished...))
	if second.state.Coins != 100 || second.credits != 0 {
		t.Fatalf("relaunch: coins %d after %d credits, want 100 after 0", second.state.Coins, second.credits)
	}
	if len(second.app.Unfinished) != 0 || !slices.Equal(second.app.Finished, []string{tr}) {
		t.Fatalf("relaunch: unfinished %v, finished %v", second.app.Unfinished, second.app.Finished)
	}
	if len(second.errs) != 0 {
		t.Fatalf("errors %v", second.errs)
	}
}

// The app is killed before the credit reached the save: the next run is handed the transaction again and credits it then.
func TestAConsumableKilledBeforeSavingIsCreditedOnTheNextLaunch(t *testing.T) {
	tr := core.Transaction{ID: "2000000007", OriginalID: "2000000007", ProductID: "coins.100", ProductType: core.ProductConsumable, Quantity: 3, Signed: "jws", Verified: true}
	s := open(t, nsgotest.UnfinishedTransactions(tr))
	if s.state.Coins != 300 || s.credits != 1 {
		t.Fatalf("coins %d after %d credits, want 300 after 1", s.state.Coins, s.credits)
	}
	if got := s.saved(t); got.Coins != 300 || !got.Purchases.HasCredited(tr.ID) {
		t.Fatalf("saved %+v", got)
	}
	if !slices.Equal(s.app.Finished, []string{tr.ID}) {
		t.Fatalf("finished %v", s.app.Finished)
	}
	// The same transaction again, as a store that redelivers it, credits nothing.
	s.app.UpdatePurchase(tr)
	if s.state.Coins != 300 || s.credits != 1 {
		t.Fatalf("redelivered: coins %d after %d credits", s.state.Coins, s.credits)
	}
}

func TestAFailedSaveLeavesTheTransactionUnfinishedAndCreditsItOnce(t *testing.T) {
	s := open(t)
	s.saveErr = errors.New("disk full")
	res, err := s.buy("coins.100", 1)
	if res != liveops.BuyFailed || err == nil || err.Error() != "disk full" {
		t.Fatalf("buy: %v, %v", res, err)
	}
	if len(s.app.Unfinished) != 1 || len(s.app.Finished) != 0 {
		t.Fatalf("unfinished %v, finished %v", s.app.Unfinished, s.app.Finished)
	}
	s.saveErr = nil
	s.app.UpdatePurchase(s.app.Unfinished[0])
	if s.state.Coins != 100 || s.credits != 1 {
		t.Fatalf("coins %d after %d credits, want 100 after 1", s.state.Coins, s.credits)
	}
	if len(s.app.Unfinished) != 0 || s.saved(t).Coins != 100 {
		t.Fatalf("unfinished %v, saved %+v", s.app.Unfinished, s.saved(t))
	}
}

func TestAskToBuyWaitsThenDeliversTheApprovedPurchase(t *testing.T) {
	s := open(t)
	s.app.PurchaseAnswer = func(core.PurchaseRequest) (core.Transaction, error) { return core.Transaction{}, core.PurchasePending }
	var pending []bool
	s.cx.Effect(func() { pending = append(pending, s.purchases.Pending("coins.100")) })
	if res, err := s.buy("coins.100", 1); res != liveops.BuyPending || err != nil {
		t.Fatalf("buy: %v, %v", res, err)
	}
	if !s.purchases.Pending("coins.100") || s.state.Coins != 0 || len(s.app.Unfinished) != 0 {
		t.Fatalf("pending %v, coins %d, unfinished %v", s.purchases.Pending("coins.100"), s.state.Coins, s.app.Unfinished)
	}
	s.app.PurchaseAnswer = nil
	// A parent approves it later, and the store hands it to the app.
	s.app.UpdatePurchase(core.Transaction{ProductID: "coins.100", Quantity: 1})
	if s.purchases.Pending("coins.100") || s.state.Coins != 100 || len(s.app.Unfinished) != 0 {
		t.Fatalf("approved: pending %v, coins %d, unfinished %v", s.purchases.Pending("coins.100"), s.state.Coins, s.app.Unfinished)
	}
	if len(pending) < 3 || pending[0] || !pending[1] || pending[len(pending)-1] {
		t.Fatalf("an effect saw pending %v", pending)
	}
}

func TestCancelAndFailureAnswerApart(t *testing.T) {
	s := open(t)
	s.app.PurchaseAnswer = func(core.PurchaseRequest) (core.Transaction, error) {
		return core.Transaction{}, core.PurchaseCancelled
	}
	if res, err := s.buy("coins.100", 1); res != liveops.BuyCancelled || err != nil {
		t.Fatalf("cancel: %v, %v", res, err)
	}
	s.app.PurchaseAnswer = nil
	if res, err := s.buy("gone", 1); res != liveops.BuyFailed || !errors.Is(err, core.PurchaseFailed) {
		t.Fatalf("unknown product: %v, %v", res, err)
	}
	if s.state.Coins != 0 || s.purchases.Pending("coins.100") {
		t.Fatalf("coins %d", s.state.Coins)
	}
}

func TestBuyingAnUnlockOwnsItAndARefundTakesItBack(t *testing.T) {
	s := open(t)
	var owned []bool
	s.cx.Effect(func() { owned = append(owned, s.purchases.Owned("unlock.noads")) })
	if res, err := s.buy("unlock.noads", 1); res != liveops.Bought || err != nil {
		t.Fatalf("buy: %v, %v", res, err)
	}
	if !s.purchases.Owned("unlock.noads") || s.credits != 0 || len(s.app.Unfinished) != 0 {
		t.Fatalf("owned %v, credits %d, unfinished %v", s.purchases.Owned("unlock.noads"), s.credits, s.app.Unfinished)
	}
	tr, ok := s.purchases.Entitlement("unlock.noads")
	if !ok {
		t.Fatal("no entitlement")
	}
	tr.RevocationDate = s.cx.WallTime()
	s.app.UpdatePurchase(tr)
	if s.purchases.Owned("unlock.noads") {
		t.Fatal("still owned after a refund")
	}
	if len(owned) < 3 || owned[0] || !owned[1] || owned[len(owned)-1] {
		t.Fatalf("an effect saw owned %v", owned)
	}
}

func TestARefundedConsumableIsRevokedOnce(t *testing.T) {
	s := open(t)
	if res, err := s.buy("coins.100", 2); res != liveops.Bought || err != nil {
		t.Fatalf("buy: %v, %v", res, err)
	}
	id := s.app.Finished[0]
	refund := core.Transaction{ID: id, ProductID: "coins.100", ProductType: core.ProductConsumable, Quantity: 2, RevocationDate: s.cx.WallTime()}
	s.app.UpdatePurchase(refund)
	s.app.UpdatePurchase(refund)
	if s.state.Coins != 0 || !s.saved(t).Purchases.HasRevoked(id) {
		t.Fatalf("coins %d, saved %+v", s.state.Coins, s.saved(t))
	}
	// A refund of a purchase never credited takes nothing.
	s.app.UpdatePurchase(core.Transaction{ID: "2000000099", ProductID: "coins.100", ProductType: core.ProductConsumable, RevocationDate: s.cx.WallTime()})
	if s.state.Coins != 0 {
		t.Fatalf("coins %d", s.state.Coins)
	}
}

// A reinstall starts with no save; Restore brings the unlock back from the store.
func TestRestoreBringsBackAnUnlockOnReinstall(t *testing.T) {
	first := open(t)
	if res, err := first.buy("unlock.noads", 1); res != liveops.Bought || err != nil {
		t.Fatalf("buy: %v, %v", res, err)
	}
	account := slices.Clone(first.app.Entitlements)

	reinstalled := open(t)
	if reinstalled.purchases.Owned("unlock.noads") {
		t.Fatal("owned before the store said so")
	}
	reinstalled.app.Entitlements = account
	var err error = errors.New("unanswered")
	reinstalled.purchases.Restore(func(e error) { err = e })
	reinstalled.app.Settle()
	if err != nil || !reinstalled.purchases.Owned("unlock.noads") {
		t.Fatalf("restore: %v, owned %v", err, reinstalled.purchases.Owned("unlock.noads"))
	}
	if reinstalled.state.Coins != 0 || reinstalled.credits != 0 {
		t.Fatalf("restore credited %d coins", reinstalled.state.Coins)
	}
	var cmds []core.PurchaseCommand
	for _, r := range reinstalled.app.PurchaseRequests {
		cmds = append(cmds, r.Command)
	}
	if !slices.Equal(cmds, []core.PurchaseCommand{core.PurchaseEntitlements, core.PurchaseSync, core.PurchaseEntitlements}) {
		t.Fatalf("requests %v", cmds)
	}

	// A user who declines to sign in keeps what they had.
	reinstalled.app.SyncAnswer = core.PurchaseCancelled
	reinstalled.purchases.Restore(func(e error) { err = e })
	reinstalled.app.Settle()
	if !errors.Is(err, core.PurchaseCancelled) || !reinstalled.purchases.Owned("unlock.noads") {
		t.Fatalf("declined: %v, owned %v", err, reinstalled.purchases.Owned("unlock.noads"))
	}
}

func TestASubscriptionIsOwnedUntilItExpires(t *testing.T) {
	s := open(t)
	if res, err := s.buy("pass.monthly", 1); res != liveops.Bought || err != nil {
		t.Fatalf("buy: %v, %v", res, err)
	}
	if !s.purchases.Owned("pass.monthly") {
		t.Fatal("not owned")
	}
	tr, _ := s.purchases.Entitlement("pass.monthly")
	tr.ExpirationDate = s.cx.WallTime().Add(-time.Hour)
	s.app.UpdatePurchase(tr)
	if s.purchases.Owned("pass.monthly") {
		t.Fatal("owned after expiring")
	}
}

func TestTheServerVerifiesBeforeDelivery(t *testing.T) {
	s := open(t)
	var asked []core.Transaction
	var answer func(error)
	s.verify = func(tr core.Transaction, done func(error)) {
		asked = append(asked, tr)
		answer = done
	}
	var res liveops.BuyResult = -1
	s.purchases.Buy("coins.100", 1, func(r liveops.BuyResult, err error) { res = r })
	s.app.Settle()
	if len(asked) != 1 || asked[0].Signed == "" || s.state.Coins != 0 || res != -1 {
		t.Fatalf("asked %v, coins %d, result %v", asked, s.state.Coins, res)
	}
	// The same transaction from the store's updates waits for that answer rather than asking again.
	s.app.UpdatePurchase(asked[0])
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	answer(nil)
	answer(errors.New("late")) // a second answer is ignored
	s.app.Settle()
	if res != liveops.Bought || s.state.Coins != 100 || s.credits != 1 || len(s.app.Unfinished) != 0 {
		t.Fatalf("result %v, coins %d, credits %d, unfinished %v", res, s.state.Coins, s.credits, s.app.Unfinished)
	}

	// A refused transaction is neither credited nor finished, and one from the updates is reported.
	s.verify = func(tr core.Transaction, done func(error)) { done(fmt.Errorf("bad signature on %s", tr.ID)) }
	if res, err := s.buy("coins.100", 1); res != liveops.BuyFailed || err == nil {
		t.Fatalf("refused buy: %v, %v", res, err)
	}
	s.app.UpdatePurchase(core.Transaction{ProductID: "coins.100"})
	if s.state.Coins != 100 || len(s.app.Unfinished) != 2 || len(s.errs) != 1 {
		t.Fatalf("coins %d, unfinished %v, errors %v", s.state.Coins, s.app.Unfinished, s.errs)
	}
}

func TestWithoutAServerOnlyDeviceVerifiedTransactionsAreDelivered(t *testing.T) {
	var errs []error
	var state progress
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		liveops.NewPurchases(cx, liveops.PurchasesOptions{
			Ledger:  &state.Purchases,
			Credit:  func(tr core.Transaction) { state.Coins += 100 },
			Save:    func() error { return nil },
			OnError: func(_ core.Transaction, err error) { errs = append(errs, err) },
		})
	}, storeKit, nsgotest.UnfinishedTransactions(
		core.Transaction{ID: "1", ProductID: "coins.100", ProductType: core.ProductConsumable, Quantity: 1, Verified: true},
		core.Transaction{ID: "2", ProductID: "coins.100", ProductType: core.ProductConsumable, Quantity: 1},
	))
	if state.Coins != 100 || len(errs) != 1 || !slices.Equal(app.Finished, []string{"1"}) {
		t.Fatalf("coins %d, errors %v, finished %v", state.Coins, errs, app.Finished)
	}
}

func TestPurchasesWhereNoStoreSells(t *testing.T) {
	var p *liveops.Purchases
	app := nsgotest.StartApp(t, func(cx *core.Scope) {
		p = liveops.NewPurchases(cx, liveops.PurchasesOptions{Ledger: &liveops.Ledger{}})
	})
	var res liveops.BuyResult = -1
	var err error
	p.Buy("coins.100", 1, func(r liveops.BuyResult, e error) { res, err = r, e })
	app.Settle()
	if p.Available() || res != liveops.BuyFailed || !errors.Is(err, core.PurchasesUnavailable) || len(app.PurchaseRequests) != 0 {
		t.Fatalf("available %v, buy %v, %v, requests %v", p.Available(), res, err, app.PurchaseRequests)
	}
}

func TestBuyWithoutALedgerFails(t *testing.T) {
	var p *liveops.Purchases
	app := nsgotest.StartApp(t, func(cx *core.Scope) { p = liveops.NewPurchases(cx, liveops.PurchasesOptions{}) }, storeKit)
	var err error
	p.Buy("coins.100", 1, func(_ liveops.BuyResult, e error) { err = e })
	app.Settle()
	if !errors.Is(err, liveops.ErrNoLedger) {
		t.Fatalf("buy: %v", err)
	}
}

func TestLedgerRoundTripsAndStaysBounded(t *testing.T) {
	var l liveops.Ledger
	data, err := json.Marshal(l)
	if err != nil || string(data) != "{}" {
		t.Fatalf("empty ledger %s, %v", data, err)
	}
	l = liveops.Ledger{Credited: []string{"1", "2"}, Revoked: []string{"1"}}
	data, _ = json.Marshal(l)
	var back liveops.Ledger
	if err := json.Unmarshal(data, &back); err != nil || !slices.Equal(back.Credited, l.Credited) || !slices.Equal(back.Revoked, l.Revoked) {
		t.Fatalf("round trip %s: %+v, %v", data, back, err)
	}
	if again, _ := json.Marshal(back); string(again) != string(data) {
		t.Fatalf("%s then %s", data, again)
	}

	// A long-lived save keeps only the newest LedgerLimit credits.
	var state progress
	n := liveops.LedgerLimit + 5
	txs := make([]core.Transaction, n)
	for i := range txs {
		txs[i] = core.Transaction{ID: fmt.Sprint(3000000000 + i), ProductID: "coins.100", ProductType: core.ProductConsumable, Quantity: 1, Verified: true}
	}
	nsgotest.StartApp(t, func(cx *core.Scope) {
		liveops.NewPurchases(cx, liveops.PurchasesOptions{
			Ledger: &state.Purchases,
			Credit: func(core.Transaction) { state.Coins++ },
			Save:   func() error { return nil },
		})
	}, storeKit, nsgotest.UnfinishedTransactions(txs...))
	if state.Coins != n || len(state.Purchases.Credited) != liveops.LedgerLimit || state.Purchases.Credited[0] != txs[5].ID || !state.Purchases.HasCredited(txs[n-1].ID) {
		t.Fatalf("coins %d, ledger of %d from %s", state.Coins, len(state.Purchases.Credited), state.Purchases.Credited[0])
	}
}

func TestBuyResultStrings(t *testing.T) {
	for r, want := range map[liveops.BuyResult]string{liveops.BuyFailed: "failed", liveops.Bought: "bought", liveops.BuyPending: "pending", liveops.BuyCancelled: "cancelled"} {
		if r.String() != want {
			t.Errorf("%d: %q, want %q", r, r.String(), want)
		}
	}
}
