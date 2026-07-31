package apikey

import (
	"strings"
	"testing"
)

func TestNewTokenIsPrefixedAndUnique(t *testing.T) {
	first, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "hc_live_") || !strings.HasPrefix(second, "hc_live_") {
		t.Fatal("token prefix missing")
	}
	if first == second {
		t.Fatal("two generated tokens matched")
	}
	if tokenHash(first) == tokenHash(second) {
		t.Fatal("two token hashes matched")
	}
}

func TestUniqueStringsDropsBlanksAndDuplicates(t *testing.T) {
	got := uniqueStrings([]string{"ticket.manage", "", "ticket.manage", "conversation.read"})
	if len(got) != 2 || got[0] != "ticket.manage" || got[1] != "conversation.read" {
		t.Fatalf("uniqueStrings = %#v", got)
	}
}
