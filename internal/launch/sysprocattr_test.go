package launch

import "testing"

func TestSysProcAttrIsSet(t *testing.T) {
	if sysProcAttr() == nil {
		t.Fatal("sysProcAttr() returned nil; the launched Godot process must get an explicit SysProcAttr")
	}
}
