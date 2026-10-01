package runtime

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/example/game-server-control-plane/internal/model"
)

type Docker struct {
	binary string
}

func NewDocker(binary string) *Docker {
	return &Docker{binary: binary}
}

func (d *Docker) Create(ctx context.Context, spec Spec) error {
	args := []string{"create", "--name", containerName(spec.ID), "--restart", "no", "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m", "--security-opt", "no-new-privileges", "--cap-drop", "ALL", "--pids-limit", "512", "--log-opt", "max-size=10m", "--log-opt", "max-file=3", "--memory", strconv.FormatInt(spec.MemoryBytes, 10), "--cpu-period", "100000", "--cpu-quota", strconv.FormatInt(spec.CPUQuota, 10), "-v", spec.InstallPath + ":/srv/server"}
	for key, value := range spec.Environment {
		if strings.ContainsRune(key, '=') || strings.ContainsRune(key, '\x00') || strings.ContainsRune(value, '\x00') {
			return fmt.Errorf("invalid environment variable")
		}
		args = append(args, "-e", key+"="+value)
	}
	for _, port := range spec.Ports {
		args = append(args, "-p", fmt.Sprintf("%d:%d/%s", port.HostPort, port.TargetPort, port.Protocol))
	}
	args = append(args, spec.Image, spec.Binary)
	args = append(args, spec.Args...)
	_, err := d.run(ctx, args...)
	return err
}

func (d *Docker) Delete(ctx context.Context, id string) error {
	_, err := d.run(ctx, "rm", "-f", containerName(id))
	if isMissingContainer(err) {
		return nil
	}
	return err
}

func (d *Docker) Start(ctx context.Context, id string) error {
	_, err := d.run(ctx, "start", containerName(id))
	return err
}

func (d *Docker) Stop(ctx context.Context, id string, force bool) error {
	var err error
	if force {
		_, err = d.run(ctx, "kill", containerName(id))
	} else {
		_, err = d.run(ctx, "stop", "--time", "20", containerName(id))
	}
	return err
}

func (d *Docker) Restart(ctx context.Context, id string) error {
	_, err := d.run(ctx, "restart", "--time", "20", containerName(id))
	return err
}

func (d *Docker) Status(ctx context.Context, id string) (model.RuntimeStatus, error) {
	output, err := d.run(ctx, "inspect", "--format", "{{.State.Running}}|{{.State.StartedAt}}|{{.RestartCount}}", containerName(id))
	if err != nil {
		if isMissingContainer(err) {
			return model.RuntimeStatus{State: model.StateStopped}, nil
		}
		return model.RuntimeStatus{}, err
	}
	parts := strings.Split(strings.TrimSpace(output), "|")
	if len(parts) != 3 {
		return model.RuntimeStatus{}, fmt.Errorf("unexpected docker inspect output")
	}
	running := parts[0] == "true"
	state := model.StateStopped
	var uptime time.Duration
	if running {
		state = model.StateRunning
		if startedAt, err := time.Parse(time.RFC3339Nano, parts[1]); err == nil {
			uptime = time.Since(startedAt)
		}
	}
	restartCount, _ := strconv.Atoi(parts[2])
	status := model.RuntimeStatus{State: state, Uptime: uptime, RestartCount: restartCount, Healthy: running}
	if running {
		if stats, err := d.run(ctx, "stats", "--no-stream", "--format", "{{.CPUPerc}}|{{.MemUsage}}", containerName(id)); err == nil {
			status.CPUPercent, status.MemoryBytes = parseStats(stats)
		}
	}
	return status, nil
}

func (d *Docker) Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error) {
	if tail < 1 {
		tail = 200
	}
	if tail > 5000 {
		tail = 5000
	}
	args := []string{"logs", "--tail", strconv.Itoa(tail)}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, containerName(id))
	cmd := exec.CommandContext(ctx, d.binary, args...)
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			_ = writer.CloseWithError(err)
			return
		}
		_ = writer.Close()
	}()
	return &cappedReadCloser{ReadCloser: reader, remaining: 32 << 20}, nil
}

type cappedReadCloser struct {
	io.ReadCloser
	remaining int64
}

func (r *cappedReadCloser) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.ReadCloser.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func (d *Docker) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, d.binary, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return output.String(), fmt.Errorf("docker %s: %w: %s", args[0], err, capped(output.String(), 4096))
	}
	return output.String(), nil
}

func containerName(id string) string {
	return "gscp-" + id
}

func isMissingContainer(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such container")
}

func parseStats(value string) (float64, int64) {
	scanner := bufio.NewScanner(strings.NewReader(value))
	if !scanner.Scan() {
		return 0, 0
	}
	parts := strings.Split(scanner.Text(), "|")
	if len(parts) != 2 {
		return 0, 0
	}
	cpu, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[0]), "%"), 64)
	memoryPart := strings.TrimSpace(strings.Split(parts[1], "/")[0])
	memory := parseByteUnit(memoryPart)
	return cpu, memory
}

func parseByteUnit(value string) int64 {
	value = strings.TrimSpace(value)
	units := []struct {
		suffix string
		factor float64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"B", 1}}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, unit.suffix)), 64)
			if err == nil {
				return int64(n * unit.factor)
			}
		}
	}
	return 0
}

func capped(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
