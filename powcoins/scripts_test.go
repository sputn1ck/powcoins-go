package powcoins

import "testing"

func TestFaucetScriptsAddresses(t *testing.T) {
	scripts, err := FaucetScripts()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"tb1pruektj90gg8nysa7yuk07w7ucwlywrf4p02lq3sz49f05xd00djscyt2fw",
		"tb1pffsyra2t3nut94yvdae9evz3feg7tel843pfcv76vt5cwewavtesl3gsph",
		"tb1pf2v25yk7m8mv203pvjusmk2a8r6tu8p59nhvwux86ck3s3pp0nkqt30dvt",
	}
	if len(scripts) != len(want) {
		t.Fatalf("got %d scripts, want %d", len(scripts), len(want))
	}
	for i := range scripts {
		if scripts[i].Address != want[i] {
			t.Fatalf("script %d address got %s want %s", i, scripts[i].Address, want[i])
		}
	}
}
