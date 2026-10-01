package garrysmod

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/example/game-server-control-plane/internal/model"
)

func TestAdapterValidateAndPrepare(t *testing.T) {
	root := t.TempDir()
	server := model.GameServer{InstallPath: root, Map: "gm_construct", Gamemode: "sandbox", MaxPlayers: 16, Hostname: "Test Server", RCONPassword: "very-secure-rcon", ServerPassword: "optional"}
	ports := []model.ServerPort{{Name: "game", HostPort: 27015, TargetPort: 27015, Protocol: "udp"}}
	adapter := New()
	if err := adapter.Validate(server, ports); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Prepare(context.Background(), server); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "garrysmod", "cfg", "server.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("server.cfg is empty")
	}
	args := adapter.StartupArgs(server)
	expectedPrefix := []string{"-console", "-game", "garrysmod", "+map", "gm_construct"}
	if !reflect.DeepEqual(args[:len(expectedPrefix)], expectedPrefix) {
		t.Fatalf("unexpected startup args: %v", args)
	}
}

func TestAdapterRejectsUnsafeMap(t *testing.T) {
	server := model.GameServer{Map: "gm_construct;quit", Gamemode: "sandbox", MaxPlayers: 16, Hostname: "Test", RCONPassword: "very-secure-rcon"}
	ports := []model.ServerPort{{Name: "game", HostPort: 27015, TargetPort: 27015, Protocol: "udp"}}
	if err := New().Validate(server, ports); err == nil {
		t.Fatal("unsafe map accepted")
	}
}

func TestAdapterRejectsManagedArgumentOverride(t *testing.T) {
	server := model.GameServer{Map: "gm_construct", Gamemode: "sandbox", MaxPlayers: 16, Hostname: "Test", RCONPassword: "very-secure-rcon", ExtraArgs: []string{"+map=gm_flatgrass"}}
	ports := []model.ServerPort{{Name: "game", HostPort: 27015, TargetPort: 27015, Protocol: "udp"}}
	if err := New().Validate(server, ports); err == nil {
		t.Fatal("managed map override accepted")
	}
}
