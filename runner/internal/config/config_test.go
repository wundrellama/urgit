package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestPinIsPersistentAndNeverReplaced(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "config.toml")
	initial := "ship_url='http://ship'\nwork_dir='" + folder + "'\nstate_file='" + folder + "/state.json'\nsandbox='docker-rootless'\ndocker_host='unix:///test.sock'\nenroll_token='fixture-token'\noperator_setting='preserved'\nci_pub=''\n"
	if e := os.WriteFile(path, []byte(initial), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = cfg.PinCIPub("0x123"); e != nil {
		t.Fatal(e)
	}
	if e = cfg.PinCIPub("0x456"); e != nil {
		t.Fatal(e)
	}
	again, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if again.CIPub != "0x123" {
		t.Fatal("pin was not preserved")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "fixture-token") {
		t.Fatal("enrollment token retained while pinning")
	}
	var fields map[string]any
	if _, e = toml.Decode(string(data), &fields); e != nil {
		t.Fatal(e)
	}
	if fields["operator_setting"] != "preserved" {
		t.Fatal("unrelated setting lost")
	}
	info, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("config mode is not 0600")
	}
}
