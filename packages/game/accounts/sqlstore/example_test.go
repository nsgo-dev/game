//go:build !js && !wasip1

package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/accounts"
	"github.com/nsgo-dev/game/packages/game/accounts/sqlitestore"
	"github.com/nsgo-dev/game/packages/game/accounts/sqlstore"
)

// New creates the accounts schema on an open database and returns a store on it; a Dialect tells it how the database numbers parameters and reports a unique violation. The sqlitestore and pgstore packages call New with their dialects; this example uses SQLite's, with the driver sqlitestore registers.
func ExampleNew() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Println(err)
		return
	}
	// An in-memory SQLite database exists per connection, so keep to one.
	db.SetMaxOpenConns(1)
	store, err := sqlstore.New(ctx, db, sqlitestore.Dialect)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer store.Close()

	fmt.Println(store.CreateAccount(ctx, accounts.Account{ID: "a1", Email: "ada@example.com"}, ""))
	fmt.Println(store.CreateAccount(ctx, accounts.Account{ID: "a2", Email: "ada@example.com"}, ""))
	// Output:
	// <nil>
	// accounts: already belongs to another account
}
