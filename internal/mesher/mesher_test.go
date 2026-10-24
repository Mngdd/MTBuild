package mesher

import "testing"

func TestParamsBool(t *testing.T) {
	spec := ParamSpec{Key: "f", Kind: KindBool, Def: 1}
	if !(Params{}).Bool(spec) {
		t.Error("missing key should fall back to Def=1")
	}
	if (Params{"f": 0}).Bool(spec) {
		t.Error("0 should be false")
	}
	if !(Params{"f": 1}).Bool(spec) {
		t.Error("1 should be true")
	}
}
