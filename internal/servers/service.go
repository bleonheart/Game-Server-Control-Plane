package servers

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/example/game-server-control-plane/internal/database"
	"github.com/example/game-server-control-plane/internal/games"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/example/game-server-control-plane/internal/observability"
	"github.com/example/game-server-control-plane/internal/runtime"
	"github.com/example/game-server-control-plane/internal/secrets"
	"github.com/example/game-server-control-plane/internal/steamcmd"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var serverName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{2,63}$`)
var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var portName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)

var ErrNotFound = errors.New("server not found")
var ErrConflict = errors.New("operation conflicts with current server state")

type CreateInput struct {
	Name                 string
	Game                 string
	Map                  string
	Gamemode             string
	MaxPlayers           int
	Hostname             string
	WorkshopCollection   string
	RCONPassword         string
	ServerPassword       string
	Environment          map[string]string
	ExtraArgs            []string
	CPUQuota             int64
	MemoryBytes          int64
	AutoRecover          bool
	MaxRestartAttempts   int
	BackupRetentionCount int
	Ports                []model.ServerPort
}

type Service struct {
	db             *pgxpool.Pool
	runtime        runtime.Runtime
	steam          steamcmd.Client
	games          *games.Registry
	cipher         *secrets.Cipher
	metrics        *observability.Metrics
	dataRoot       string
	dockerDataRoot string
	gameImage      string
	portMin        int
	portMax        int
}

func NewService(db *pgxpool.Pool, rt runtime.Runtime, steam steamcmd.Client, registry *games.Registry, cipher *secrets.Cipher, metrics *observability.Metrics, dataRoot, dockerDataRoot, gameImage string, portMin, portMax int) *Service {
	return &Service{db: db, runtime: rt, steam: steam, games: registry, cipher: cipher, metrics: metrics, dataRoot: dataRoot, dockerDataRoot: dockerDataRoot, gameImage: gameImage, portMin: portMin, portMax: portMax}
}

func (s *Service) Create(ctx context.Context, input CreateInput) (model.GameServer, error) {
	if !serverName.MatchString(input.Name) {
		return model.GameServer{}, fmt.Errorf("name must be 3-64 characters using letters, numbers, dot, underscore, or dash")
	}
	adapter, ok := s.games.Get(input.Game)
	if !ok {
		return model.GameServer{}, fmt.Errorf("unsupported game")
	}
	if input.Environment == nil {
		input.Environment = map[string]string{}
	}
	if input.ExtraArgs == nil {
		input.ExtraArgs = []string{}
	}
	if len(input.Environment) > 64 {
		return model.GameServer{}, fmt.Errorf("too many environment variables")
	}
	for key, value := range input.Environment {
		if !environmentName.MatchString(key) || len(value) > 4096 || strings.ContainsRune(value, '\x00') {
			return model.GameServer{}, fmt.Errorf("invalid environment variable")
		}
	}
	if input.CPUQuota <= 0 {
		input.CPUQuota = 100000
	}
	if input.CPUQuota < 10000 || input.CPUQuota > 6400000 {
		return model.GameServer{}, fmt.Errorf("cpu quota out of range")
	}
	if input.MemoryBytes == 0 {
		input.MemoryBytes = 1 << 30
	}
	if input.MemoryBytes < 128<<20 || input.MemoryBytes > 128<<30 {
		return model.GameServer{}, fmt.Errorf("memory limit out of range")
	}
	if input.MaxRestartAttempts == 0 {
		input.MaxRestartAttempts = 5
	}
	if input.MaxRestartAttempts < 0 || input.MaxRestartAttempts > 100 {
		return model.GameServer{}, fmt.Errorf("max restart attempts out of range")
	}
	if input.BackupRetentionCount == 0 {
		input.BackupRetentionCount = 7
	}
	if input.BackupRetentionCount < 1 || input.BackupRetentionCount > 1000 {
		return model.GameServer{}, fmt.Errorf("backup retention count out of range")
	}
	id, err := database.NewID("srv")
	if err != nil {
		return model.GameServer{}, err
	}
	installPath := filepath.Join(s.dataRoot, id)
	if err := ensureWithinRoot(s.dataRoot, installPath); err != nil {
		return model.GameServer{}, err
	}
	server := model.GameServer{
		ID: id, Name: input.Name, Game: input.Game, DesiredState: model.StateStopped, ActualState: model.StateProvisioning,
		Image: s.gameImage, InstallPath: installPath, Map: input.Map, Gamemode: input.Gamemode, MaxPlayers: input.MaxPlayers,
		Hostname: input.Hostname, WorkshopCollection: input.WorkshopCollection, RCONPassword: input.RCONPassword,
		ServerPassword: input.ServerPassword, Environment: input.Environment, ExtraArgs: input.ExtraArgs, CPUQuota: input.CPUQuota,
		MemoryBytes: input.MemoryBytes, AutoRecover: input.AutoRecover, MaxRestartAttempts: input.MaxRestartAttempts, BackupRetentionCount: input.BackupRetentionCount,
	}
	seenPorts := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	for i := range input.Ports {
		input.Ports[i].ServerID = id
		if !portName.MatchString(input.Ports[i].Name) {
			return model.GameServer{}, fmt.Errorf("invalid port name")
		}
		if input.Ports[i].HostPort < s.portMin || input.Ports[i].HostPort > s.portMax {
			return model.GameServer{}, fmt.Errorf("host port outside allowed range")
		}
		if input.Ports[i].TargetPort < 1 || input.Ports[i].TargetPort > 65535 {
			return model.GameServer{}, fmt.Errorf("target port out of range")
		}
		if input.Ports[i].Protocol != "tcp" && input.Ports[i].Protocol != "udp" {
			return model.GameServer{}, fmt.Errorf("invalid port protocol")
		}
		portKey := fmt.Sprintf("%d/%s", input.Ports[i].HostPort, input.Ports[i].Protocol)
		if _, exists := seenPorts[portKey]; exists {
			return model.GameServer{}, fmt.Errorf("duplicate host port mapping")
		}
		seenPorts[portKey] = struct{}{}
		if _, exists := seenNames[input.Ports[i].Name]; exists {
			return model.GameServer{}, fmt.Errorf("duplicate port name")
		}
		seenNames[input.Ports[i].Name] = struct{}{}
	}
	if err := adapter.Validate(server, input.Ports); err != nil {
		return model.GameServer{}, err
	}
	rconEncrypted, err := s.cipher.Encrypt(server.RCONPassword)
	if err != nil {
		return model.GameServer{}, err
	}
	passwordEncrypted, err := s.cipher.Encrypt(server.ServerPassword)
	if err != nil {
		return model.GameServer{}, err
	}
	environment, _ := json.Marshal(server.Environment)
	extraArgs, _ := json.Marshal(server.ExtraArgs)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return model.GameServer{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO game_servers(id,name,game,desired_state,actual_state,image,install_path,map,gamemode,max_players,hostname,workshop_collection,rcon_password_enc,server_password_enc,environment,extra_args,cpu_quota,memory_bytes,auto_recover,max_restart_attempts,backup_retention_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`, server.ID, server.Name, server.Game, server.DesiredState, server.ActualState, server.Image, server.InstallPath, server.Map, server.Gamemode, server.MaxPlayers, server.Hostname, server.WorkshopCollection, rconEncrypted, passwordEncrypted, environment, extraArgs, server.CPUQuota, server.MemoryBytes, server.AutoRecover, server.MaxRestartAttempts, server.BackupRetentionCount); err != nil {
		return model.GameServer{}, err
	}
	for i := range input.Ports {
		portID, err := database.NewID("prt")
		if err != nil {
			return model.GameServer{}, err
		}
		input.Ports[i].ID = portID
		if _, err := tx.Exec(ctx, `INSERT INTO server_ports(id,server_id,name,host_port,target_port,protocol) VALUES($1,$2,$3,$4,$5,$6)`, portID, server.ID, input.Ports[i].Name, input.Ports[i].HostPort, input.Ports[i].TargetPort, input.Ports[i].Protocol); err != nil {
			return model.GameServer{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.GameServer{}, err
	}
	if err := os.MkdirAll(server.InstallPath, 0o750); err != nil {
		s.markFailed(ctx, server.ID, err)
		return model.GameServer{}, err
	}
	if _, err := s.steam.Install(ctx, server.InstallPath, adapter.AppID(), true); err != nil {
		s.markFailed(ctx, server.ID, err)
		return model.GameServer{}, err
	}
	if err := adapter.Prepare(ctx, server); err != nil {
		s.markFailed(ctx, server.ID, err)
		return model.GameServer{}, err
	}
	if err := s.runtime.Create(ctx, s.runtimeSpec(server, input.Ports, adapter.StartupArgs(server))); err != nil {
		s.markFailed(ctx, server.ID, err)
		return model.GameServer{}, err
	}
	if _, err := s.db.Exec(ctx, `UPDATE game_servers SET actual_state='stopped',last_error='',updated_at=now() WHERE id=$1`, server.ID); err != nil {
		return model.GameServer{}, err
	}
	server.ActualState = model.StateStopped
	server.RCONPassword = ""
	server.ServerPassword = ""
	return server, nil
}

func (s *Service) List(ctx context.Context, limit, offset int, state string) ([]model.GameServer, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query := `SELECT id,name,game,desired_state,actual_state,image,install_path,map,gamemode,max_players,hostname,workshop_collection,rcon_password_enc,server_password_enc,environment,extra_args,cpu_quota,memory_bytes,auto_recover,max_restart_attempts,backup_retention_count,restart_count,last_error,created_at,updated_at FROM game_servers`
	args := []any{}
	if state != "" {
		query += ` WHERE actual_state=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
		args = append(args, state, limit, offset)
	} else {
		query += ` ORDER BY created_at DESC LIMIT $1 OFFSET $2`
		args = append(args, limit, offset)
	}
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.GameServer, 0, limit)
	for rows.Next() {
		item, err := s.scanServer(rows)
		if err != nil {
			return nil, err
		}
		item.RCONPassword = ""
		item.ServerPassword = ""
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (model.GameServer, []model.ServerPort, error) {
	server, err := s.getInternal(ctx, id)
	if err != nil {
		return model.GameServer{}, nil, err
	}
	ports, err := s.Ports(ctx, id)
	if err != nil {
		return model.GameServer{}, nil, err
	}
	server.RCONPassword = ""
	server.ServerPassword = ""
	return server, ports, nil
}

func (s *Service) Ports(ctx context.Context, id string) ([]model.ServerPort, error) {
	rows, err := s.db.Query(ctx, `SELECT id,server_id,name,host_port,target_port,protocol FROM server_ports WHERE server_id=$1 ORDER BY name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ports []model.ServerPort
	for rows.Next() {
		var port model.ServerPort
		if err := rows.Scan(&port.ID, &port.ServerID, &port.Name, &port.HostPort, &port.TargetPort, &port.Protocol); err != nil {
			return nil, err
		}
		ports = append(ports, port)
	}
	return ports, rows.Err()
}

func (s *Service) Start(ctx context.Context, id string) error {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+id)
	if err != nil {
		return err
	}
	defer release()
	if err := s.claim(ctx, id, []model.ServerState{model.StateStopped, model.StateFailed}, model.StateRunning, model.StateStarting); err != nil {
		return err
	}
	if err := s.runtime.Start(ctx, id); err != nil {
		s.markFailed(ctx, id, err)
		return err
	}
	return s.setState(ctx, id, model.StateRunning, model.StateRunning, "")
}

func (s *Service) Stop(ctx context.Context, id string, force bool) error {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+id)
	if err != nil {
		return err
	}
	defer release()
	if err := s.claim(ctx, id, []model.ServerState{model.StateRunning, model.StateStarting, model.StateFailed}, model.StateStopped, model.StateStopping); err != nil {
		return err
	}
	if err := s.runtime.Stop(ctx, id, force); err != nil {
		s.markFailed(ctx, id, err)
		return err
	}
	return s.setState(ctx, id, model.StateStopped, model.StateStopped, "")
}

func (s *Service) Restart(ctx context.Context, id string) error {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+id)
	if err != nil {
		return err
	}
	defer release()
	if err := s.claim(ctx, id, []model.ServerState{model.StateRunning}, model.StateRunning, model.StateStarting); err != nil {
		return err
	}
	if err := s.runtime.Restart(ctx, id); err != nil {
		s.markFailed(ctx, id, err)
		return err
	}
	s.metrics.IncRestart()
	return s.setState(ctx, id, model.StateRunning, model.StateRunning, "")
}

func (s *Service) Update(ctx context.Context, id string, validate bool) error {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+id)
	if err != nil {
		return err
	}
	defer release()
	server, err := s.getInternal(ctx, id)
	if err != nil {
		return err
	}
	ports, err := s.Ports(ctx, id)
	if err != nil {
		return err
	}
	adapter, ok := s.games.Get(server.Game)
	if !ok {
		return fmt.Errorf("unsupported game")
	}
	wasRunning := server.ActualState == model.StateRunning
	if err := s.claim(ctx, id, []model.ServerState{model.StateStopped, model.StateRunning, model.StateFailed}, server.DesiredState, model.StateUpdating); err != nil {
		return err
	}
	if wasRunning {
		if err := s.runtime.Stop(ctx, id, false); err != nil {
			s.markFailedStopped(ctx, id, err)
			s.metrics.IncUpdate(false)
			return err
		}
	}
	historyID, err := database.NewID("upd")
	if err != nil {
		s.markFailedStopped(ctx, id, err)
		return err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO update_history(id,server_id,status) VALUES($1,$2,'running')`, historyID, id); err != nil {
		s.markFailedStopped(ctx, id, err)
		return err
	}
	lastProgressWrite := time.Time{}
	output, updateErr := s.steam.Update(ctx, server.InstallPath, adapter.AppID(), validate, func(progress string) {
		if time.Since(lastProgressWrite) < time.Second {
			return
		}
		lastProgressWrite = time.Now()
		_, _ = s.db.Exec(ctx, `UPDATE update_history SET output=$2 WHERE id=$1 AND status='running'`, historyID, progress)
	})
	status := "succeeded"
	if updateErr != nil {
		status = "failed"
	}
	_, _ = s.db.Exec(ctx, `UPDATE update_history SET status=$2,output=$3,finished_at=now() WHERE id=$1`, historyID, status, output)
	if updateErr != nil {
		s.markFailedStopped(ctx, id, updateErr)
		s.metrics.IncUpdate(false)
		return updateErr
	}
	if err := adapter.Prepare(ctx, server); err != nil {
		s.markFailedStopped(ctx, id, err)
		s.metrics.IncUpdate(false)
		return err
	}
	if err := s.runtime.Delete(ctx, id); err != nil {
		s.markFailedStopped(ctx, id, err)
		s.metrics.IncUpdate(false)
		return err
	}
	if err := s.runtime.Create(ctx, s.runtimeSpec(server, ports, adapter.StartupArgs(server))); err != nil {
		s.markFailedStopped(ctx, id, err)
		s.metrics.IncUpdate(false)
		return err
	}
	if wasRunning {
		if err := s.runtime.Start(ctx, id); err != nil {
			s.markFailedStopped(ctx, id, err)
			s.metrics.IncUpdate(false)
			return err
		}
	}
	s.metrics.IncUpdate(true)
	if wasRunning {
		return s.setState(ctx, id, model.StateRunning, model.StateRunning, "")
	}
	return s.setState(ctx, id, model.StateStopped, model.StateStopped, "")
}

func (s *Service) ValidateInstallation(ctx context.Context, id string) (string, error) {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+id)
	if err != nil {
		return "", err
	}
	defer release()
	server, err := s.getInternal(ctx, id)
	if err != nil {
		return "", err
	}
	if server.ActualState != model.StateStopped && server.ActualState != model.StateFailed {
		return "", ErrConflict
	}
	adapter, ok := s.games.Get(server.Game)
	if !ok {
		return "", fmt.Errorf("unsupported game")
	}
	return s.steam.Validate(ctx, server.InstallPath, adapter.AppID())
}

func (s *Service) Delete(ctx context.Context, id string) error {
	release, err := database.AcquireResourceLock(ctx, s.db, "server:"+id)
	if err != nil {
		return err
	}
	defer release()
	if err := s.claim(ctx, id, []model.ServerState{model.StateStopped, model.StateFailed}, model.StateStopped, model.StateStopping); err != nil {
		return err
	}
	server, err := s.getInternal(ctx, id)
	if err != nil {
		return err
	}
	if err := s.runtime.Delete(ctx, id); err != nil {
		s.markFailed(ctx, id, err)
		return err
	}
	if err := ensureWithinRoot(s.dataRoot, server.InstallPath); err != nil {
		return err
	}
	if err := os.RemoveAll(server.InstallPath); err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `DELETE FROM game_servers WHERE id=$1`, id)
	return err
}

func (s *Service) Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error) {
	if _, _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.runtime.Logs(ctx, id, tail, follow)
}

func (s *Service) Status(ctx context.Context, id string) (model.ServerStatus, error) {
	server, err := s.getInternal(ctx, id)
	if err != nil {
		return model.ServerStatus{}, err
	}
	ports, err := s.Ports(ctx, id)
	if err != nil {
		return model.ServerStatus{}, err
	}
	runtimeStatus, err := s.runtime.Status(ctx, id)
	if err != nil {
		return model.ServerStatus{}, err
	}
	responsive := false
	if runtimeStatus.State == model.StateRunning {
		if adapter, ok := s.games.Get(server.Game); ok {
			responsive = probeSource(ctx, adapter.HealthAddress(server, ports))
		}
	}
	updateStatus := "idle"
	var latest string
	err = s.db.QueryRow(ctx, `SELECT status FROM update_history WHERE server_id=$1 ORDER BY started_at DESC LIMIT 1`, id).Scan(&latest)
	if err == nil {
		updateStatus = latest
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return model.ServerStatus{}, err
	}
	if server.ActualState == model.StateUpdating {
		updateStatus = "running"
	}
	s.metrics.ObserveServer(id, model.RuntimeStatus{
		State:        runtimeStatus.State,
		CPUPercent:   runtimeStatus.CPUPercent,
		MemoryBytes:  runtimeStatus.MemoryBytes,
		Uptime:       runtimeStatus.Uptime,
		RestartCount: runtimeStatus.RestartCount,
		Healthy:      responsive,
	})
	return model.ServerStatus{
		DesiredState:         server.DesiredState,
		RecordedState:        server.ActualState,
		RuntimeState:         runtimeStatus.State,
		Responsive:           responsive,
		CPUPercent:           runtimeStatus.CPUPercent,
		MemoryBytes:          runtimeStatus.MemoryBytes,
		UptimeSeconds:        runtimeStatus.Uptime.Seconds(),
		RuntimeRestartCount:  runtimeStatus.RestartCount,
		RecoveryRestartCount: server.RestartCount,
		UpdateStatus:         updateStatus,
	}, nil
}

func (s *Service) Reconcile(ctx context.Context) error {
	total := 0
	running := 0
	for offset := 0; ; offset += 200 {
		items, err := s.List(ctx, 200, offset, "")
		if err != nil {
			return err
		}
		for _, server := range items {
			total++
			status, err := s.runtime.Status(ctx, server.ID)
			if err != nil {
				_, _ = s.db.Exec(ctx, `UPDATE game_servers SET last_error=$2,updated_at=now() WHERE id=$1`, server.ID, err.Error())
				continue
			}
			ports, _ := s.Ports(ctx, server.ID)
			if status.State == model.StateRunning {
				running++
				if adapter, ok := s.games.Get(server.Game); ok {
					status.Healthy = probeSource(ctx, adapter.HealthAddress(server, ports))
				}
			}
			s.metrics.ObserveServer(server.ID, status)
			if isTransitional(server.ActualState) && time.Since(server.UpdatedAt) < 2*time.Minute {
				continue
			}
			preserveFailure := server.ActualState == model.StateFailed && server.DesiredState == model.StateStopped && status.State == model.StateStopped
			if server.ActualState != status.State && !preserveFailure {
				_, _ = s.db.Exec(ctx, `UPDATE game_servers SET actual_state=$2,updated_at=now() WHERE id=$1`, server.ID, status.State)
				server.ActualState = status.State
			}
			if server.DesiredState == model.StateStopped && status.State == model.StateRunning {
				if err := s.Stop(ctx, server.ID, false); err != nil && !errors.Is(err, ErrConflict) {
					_ = s.event(ctx, server.ID, "reconcile_stop_failed", "failed to enforce stopped desired state", map[string]any{"error": err.Error()})
				}
				continue
			}
			if server.DesiredState != model.StateRunning || status.State != model.StateStopped || !server.AutoRecover {
				continue
			}
			if server.RestartCount >= server.MaxRestartAttempts {
				if server.ActualState != model.StateFailed {
					_, _ = s.db.Exec(ctx, `UPDATE game_servers SET actual_state='failed',last_error='automatic recovery attempts exhausted',updated_at=now() WHERE id=$1`, server.ID)
					_ = s.event(ctx, server.ID, "recovery_exhausted", "automatic recovery attempts exhausted", map[string]any{"attempts": server.RestartCount})
				}
				continue
			}
			eligible, err := s.recoveryEligible(ctx, server.ID, server.RestartCount)
			if err != nil || !eligible {
				continue
			}
			if err := s.Start(ctx, server.ID); err == nil {
				_, _ = s.db.Exec(ctx, `UPDATE game_servers SET restart_count=restart_count+1,updated_at=now() WHERE id=$1`, server.ID)
				_ = s.event(ctx, server.ID, "recovery", "automatic restart succeeded", map[string]any{"attempt": server.RestartCount + 1})
				s.metrics.IncRestart()
			} else if !errors.Is(err, ErrConflict) {
				_ = s.event(ctx, server.ID, "recovery_failed", "automatic restart failed", map[string]any{"error": err.Error()})
			}
		}
		if len(items) < 200 {
			break
		}
	}
	s.metrics.SetFleet(total, running)
	return nil
}

func isTransitional(state model.ServerState) bool {
	return state == model.StateProvisioning || state == model.StateStarting || state == model.StateStopping || state == model.StateUpdating
}

func (s *Service) ResetRestartCount(ctx context.Context, id string) error {
	result, err := s.db.Exec(ctx, `UPDATE game_servers SET restart_count=0,updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) UpdateHistory(ctx context.Context, id string, limit int) ([]model.UpdateHistory, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `SELECT id,server_id,status,LEFT(output,65536),started_at,finished_at FROM update_history WHERE server_id=$1 ORDER BY started_at DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.UpdateHistory
	for rows.Next() {
		var item model.UpdateHistory
		var finished stdsql.NullTime
		if err := rows.Scan(&item.ID, &item.ServerID, &item.Status, &item.Output, &item.StartedAt, &finished); err != nil {
			return nil, err
		}
		if finished.Valid {
			item.FinishedAt = &finished.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) BackupRetentionCount(ctx context.Context, id string) (int, error) {
	var count int
	if err := s.db.QueryRow(ctx, `SELECT backup_retention_count FROM game_servers WHERE id=$1`, id).Scan(&count); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return count, nil
}

func (s *Service) getInternal(ctx context.Context, id string) (model.GameServer, error) {
	row := s.db.QueryRow(ctx, `SELECT id,name,game,desired_state,actual_state,image,install_path,map,gamemode,max_players,hostname,workshop_collection,rcon_password_enc,server_password_enc,environment,extra_args,cpu_quota,memory_bytes,auto_recover,max_restart_attempts,backup_retention_count,restart_count,last_error,created_at,updated_at FROM game_servers WHERE id=$1`, id)
	server, err := s.scanServer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.GameServer{}, ErrNotFound
	}
	return server, err
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *Service) scanServer(row scanner) (model.GameServer, error) {
	var server model.GameServer
	var rconEncrypted, passwordEncrypted, environment, extraArgs []byte
	if err := row.Scan(&server.ID, &server.Name, &server.Game, &server.DesiredState, &server.ActualState, &server.Image, &server.InstallPath, &server.Map, &server.Gamemode, &server.MaxPlayers, &server.Hostname, &server.WorkshopCollection, &rconEncrypted, &passwordEncrypted, &environment, &extraArgs, &server.CPUQuota, &server.MemoryBytes, &server.AutoRecover, &server.MaxRestartAttempts, &server.BackupRetentionCount, &server.RestartCount, &server.LastError, &server.CreatedAt, &server.UpdatedAt); err != nil {
		return model.GameServer{}, err
	}
	if err := json.Unmarshal(environment, &server.Environment); err != nil {
		return model.GameServer{}, err
	}
	if err := json.Unmarshal(extraArgs, &server.ExtraArgs); err != nil {
		return model.GameServer{}, err
	}
	var err error
	server.RCONPassword, err = s.cipher.Decrypt(rconEncrypted)
	if err != nil {
		return model.GameServer{}, err
	}
	server.ServerPassword, err = s.cipher.Decrypt(passwordEncrypted)
	if err != nil {
		return model.GameServer{}, err
	}
	return server, nil
}

func (s *Service) claim(ctx context.Context, id string, allowed []model.ServerState, desired, actual model.ServerState) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var current model.ServerState
	if err := tx.QueryRow(ctx, `SELECT actual_state FROM game_servers WHERE id=$1 FOR UPDATE`, id).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	allowedCurrent := false
	for _, state := range allowed {
		if current == state {
			allowedCurrent = true
			break
		}
	}
	if !allowedCurrent {
		return fmt.Errorf("%w: current state %s", ErrConflict, current)
	}
	if _, err := tx.Exec(ctx, `UPDATE game_servers SET desired_state=$2,actual_state=$3,last_error='',updated_at=now() WHERE id=$1`, id, desired, actual); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) setState(ctx context.Context, id string, desired, actual model.ServerState, lastError string) error {
	_, err := s.db.Exec(ctx, `UPDATE game_servers SET desired_state=$2,actual_state=$3,last_error=$4,updated_at=now() WHERE id=$1`, id, desired, actual, lastError)
	return err
}

func (s *Service) markFailedStopped(ctx context.Context, id string, cause error) {
	message := cause.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	_, _ = s.db.Exec(ctx, `UPDATE game_servers SET desired_state='stopped',actual_state='failed',last_error=$2,updated_at=now() WHERE id=$1`, id, message)
}

func (s *Service) markFailed(ctx context.Context, id string, cause error) {
	message := cause.Error()
	if len(message) > 2048 {
		message = message[:2048]
	}
	_, _ = s.db.Exec(ctx, `UPDATE game_servers SET actual_state='failed',last_error=$2,updated_at=now() WHERE id=$1`, id, message)
}

func (s *Service) event(ctx context.Context, serverID, kind, message string, metadata map[string]any) error {
	id, err := database.NewID("evt")
	if err != nil {
		return err
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO server_events(id,server_id,kind,message,metadata) VALUES($1,$2,$3,$4,$5)`, id, serverID, kind, message, body)
	return err
}

func (s *Service) recoveryEligible(ctx context.Context, id string, count int) (bool, error) {
	var last time.Time
	err := s.db.QueryRow(ctx, `SELECT COALESCE(MAX(created_at), to_timestamp(0)) FROM server_events WHERE server_id=$1 AND kind IN ('recovery','recovery_failed')`, id).Scan(&last)
	if err != nil {
		return false, err
	}
	delay := time.Second * time.Duration(1<<min(count, 8))
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	return time.Since(last) >= delay, nil
}

func (s *Service) runtimeSpec(server model.GameServer, ports []model.ServerPort, args []string) runtime.Spec {
	mapped := make([]runtime.PortMapping, 0, len(ports))
	for _, port := range ports {
		mapped = append(mapped, runtime.PortMapping{HostPort: port.HostPort, TargetPort: port.TargetPort, Protocol: port.Protocol})
	}
	mountPath := filepath.Join(s.dockerDataRoot, server.ID)
	return runtime.Spec{ID: server.ID, Image: server.Image, InstallPath: mountPath, Binary: "/srv/server/srcds_run", Args: args, Environment: server.Environment, Ports: mapped, CPUQuota: server.CPUQuota, MemoryBytes: server.MemoryBytes}
}

func ensureWithinRoot(root, path string) error {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
		return nil
	}
	return fmt.Errorf("path outside data root")
}

func probeSource(ctx context.Context, address string) bool {
	if address == "" {
		return false
	}
	dialer := net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	query := []byte{0xff, 0xff, 0xff, 0xff, 'T', 'S', 'o', 'u', 'r', 'c', 'e', ' ', 'E', 'n', 'g', 'i', 'n', 'e', ' ', 'Q', 'u', 'e', 'r', 'y', 0x00}
	if _, err := conn.Write(query); err != nil {
		return false
	}
	buffer := make([]byte, 1400)
	n, err := conn.Read(buffer)
	return err == nil && n >= 5 && buffer[0] == 0xff && buffer[1] == 0xff && buffer[2] == 0xff && buffer[3] == 0xff
}
