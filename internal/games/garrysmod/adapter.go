package garrysmod

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/example/game-server-control-plane/internal/model"
)

var safeToken = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var workshopID = regexp.MustCompile(`^[0-9]{1,20}$`)

type Adapter struct{}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Name() string {
	return "garrysmod"
}

func (a *Adapter) AppID() string {
	return "4020"
}

func (a *Adapter) Validate(server model.GameServer, ports []model.ServerPort) error {
	if !safeToken.MatchString(server.Map) {
		return fmt.Errorf("invalid map")
	}
	if !safeToken.MatchString(server.Gamemode) {
		return fmt.Errorf("invalid gamemode")
	}
	if server.MaxPlayers < 1 || server.MaxPlayers > 128 {
		return fmt.Errorf("max players out of range")
	}
	if len(server.Hostname) < 1 || len(server.Hostname) > 128 || strings.ContainsAny(server.Hostname, "\r\n\x00") {
		return fmt.Errorf("invalid hostname")
	}
	if server.WorkshopCollection != "" && !workshopID.MatchString(server.WorkshopCollection) {
		return fmt.Errorf("invalid workshop collection")
	}
	if len(server.RCONPassword) < 12 || strings.ContainsAny(server.RCONPassword, "\r\n\x00\"") {
		return fmt.Errorf("rcon password must be at least 12 safe characters")
	}
	if strings.ContainsAny(server.ServerPassword, "\r\n\x00\"") {
		return fmt.Errorf("invalid server password")
	}
	if len(server.ExtraArgs) > 64 {
		return fmt.Errorf("too many extra arguments")
	}
	blockedArgs := map[string]struct{}{
		"+rcon_password": {}, "+sv_password": {}, "-port": {}, "+hostport": {}, "-ip": {}, "+map": {}, "+gamemode": {}, "+maxplayers": {}, "+host_workshop_collection": {},
	}
	for _, arg := range server.ExtraArgs {
		if len(arg) > 256 || strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("invalid extra argument")
		}
		lower := strings.ToLower(arg)
		for blocked := range blockedArgs {
			if lower == blocked || strings.HasPrefix(lower, blocked+"=") {
				return fmt.Errorf("extra argument conflicts with managed settings")
			}
		}
	}
	gamePort := false
	for _, port := range ports {
		if port.Name == "game" && port.TargetPort == 27015 && port.Protocol == "udp" {
			gamePort = true
		}
	}
	if !gamePort {
		return fmt.Errorf("garrysmod requires a game UDP port targeting 27015")
	}
	return nil
}

func (a *Adapter) Prepare(ctx context.Context, server model.GameServer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfgDir := filepath.Join(server.InstallPath, "garrysmod", "cfg")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		return err
	}
	content := strings.Join([]string{
		`hostname "` + escapeConfig(server.Hostname) + `"`,
		`rcon_password "` + escapeConfig(server.RCONPassword) + `"`,
		`sv_password "` + escapeConfig(server.ServerPassword) + `"`,
		"sv_lan 0",
		"log on",
	}, "\n") + "\n"
	return os.WriteFile(filepath.Join(cfgDir, "server.cfg"), []byte(content), 0o600)
}

func (a *Adapter) StartupArgs(server model.GameServer) []string {
	args := []string{
		"-console",
		"-game", "garrysmod",
		"+map", server.Map,
		"+gamemode", server.Gamemode,
		"+maxplayers", strconv.Itoa(server.MaxPlayers),
	}
	if server.WorkshopCollection != "" {
		args = append(args, "+host_workshop_collection", server.WorkshopCollection)
	}
	args = append(args, server.ExtraArgs...)
	return args
}

func (a *Adapter) HealthAddress(server model.GameServer, ports []model.ServerPort) string {
	for _, port := range ports {
		if port.Name == "game" {
			return net.JoinHostPort("127.0.0.1", strconv.Itoa(port.HostPort))
		}
	}
	return ""
}

func escapeConfig(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
