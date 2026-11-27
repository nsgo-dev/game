//go:build !js && !wasip1

package sqlitestore_test

import (
	"context"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/accounts"
	"github.com/nsgo-dev/game/pkg/accounts/sqlitestore"
)

// Open returns an accounts.Store in a SQLite database file; ":memory:" opens a private in-memory database, as here.
func ExampleOpen() {
	ctx := context.Background()
	store, err := sqlitestore.Open(ctx, ":memory:")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer store.Close()

	created := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	err = store.CreateAccount(ctx, accounts.Account{ID: "a1", Email: "ada@example.com", Created: created}, "")
	fmt.Println(err)
	a, _, err := store.AccountByEmail(ctx, "ada@example.com")
	fmt.Println(a.ID, err)
	_, err = store.Account(ctx, "missing")
	fmt.Println(err)
	// Output:
	// <nil>
	// a1 <nil>
	// accounts: not found
}
