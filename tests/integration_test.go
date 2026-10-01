package tests

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"

	"github.com/example/game-server-control-plane/internal/database"
	"github.com/example/game-server-control-plane/internal/games"
	"github.com/example/game-server-control-plane/internal/games/garrysmod"
	"github.com/example/game-server-control-plane/internal/model"
	"github.com/example/game-server-control-plane/internal/observability"
	gameruntime "github.com/example/game-server-control-plane/internal/runtime"
	"github.com/example/game-server-control-plane/internal/secrets"
	"github.com/example/game-server-control-plane/internal/servers"
	"github.com/example/game-server-control-plane/internal/steamcmd"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeRuntime struct {
	startCount atomic.Int64
}

func (f *fakeRuntime) Create(context.Context, gameruntime.Spec) error { return nil }
func (f *fakeRuntime) Delete(context.Context, string) error           { return nil }
func (f *fakeRuntime) Start(context.Context, string) error {
	f.startCount.Add(1)
	return nil
}
func (f *fakeRuntime) Stop(context.Context, string, bool) error { return nil }
func (f *fakeRuntime) Restart(context.Context, string) error    { return nil }
func (f *fakeRuntime) Status(context.Context, string) (model.RuntimeStatus, error) {
	return model.RuntimeStatus{State: model.StateStopped}, nil
}
func (f *fakeRuntime) Logs(context.Context, string, int, bool) (io.ReadCloser, error) {
	return io.NopCloser(&emptyReader{}), nil
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

type fakeSteam struct{}

func (fakeSteam) Install(context.Context, string, string, bool) (string, error) { return "ok", nil }
func (fakeSteam) Update(context.Context, string, string, bool, steamcmd.ProgressFunc) (string, error) {
	return "ok", nil
}
func (fakeSteam) Validate(context.Context, string, string) (string, error) { return "ok", nil }

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `TRUNCATE audit_logs,schedules,backups,update_history,server_events,server_ports,game_servers,sessions,user_roles,users CASCADE`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestIntegrationConcurrentStart(t *testing.T) {
	db := testDB(t)
	cipher, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	rt := &fakeRuntime{}
	service := servers.NewService(db, rt, fakeSteam{}, games.NewRegistry(garrysmod.New()), cipher, observability.NewMetrics(), t.TempDir(), t.TempDir(), "test-image", 20000, 40000)
	server, err := service.Create(context.Background(), servers.CreateInput{
		Name: "integration-gmod", Game: "garrysmod", Map: "gm_construct", Gamemode: "sandbox", MaxPlayers: 16,
		Hostname: "Integration", RCONPassword: "integration-rcon-password", AutoRecover: true, MaxRestartAttempts: 3,
		Ports: []model.ServerPort{{Name: "game", HostPort: 27015, TargetPort: 27015, Protocol: "udp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { results <- service.Start(context.Background(), server.ID) }()
	go func() { results <- service.Start(context.Background(), server.ID) }()
	first := <-results
	second := <-results
	if first != nil && second != nil {
		t.Fatalf("both starts failed: %v / %v", first, second)
	}
	if first == nil && second == nil {
		t.Fatal("both starts succeeded")
	}
	if first != nil && !errors.Is(first, servers.ErrConflict) {
		t.Fatalf("unexpected error: %v", first)
	}
	if second != nil && !errors.Is(second, servers.ErrConflict) {
		t.Fatalf("unexpected error: %v", second)
	}
	if rt.startCount.Load() != 1 {
		t.Fatalf("runtime start count=%d", rt.startCount.Load())
	}
}
