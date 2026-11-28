package accounts

import "testing"

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("short", 10) == nil || ValidatePassword("long enough", 10) != nil {
		t.Fatal("password policy")
	}
}
