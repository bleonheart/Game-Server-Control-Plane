package steamcmd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

type ProgressFunc func(string)

type Client interface {
	Install(context.Context, string, string, bool) (string, error)
	Update(context.Context, string, string, bool, ProgressFunc) (string, error)
	Validate(context.Context, string, string) (string, error)
}

type ExecClient struct {
	binary string
}

func New(binary string) *ExecClient {
	return &ExecClient{binary: binary}
}

func (c *ExecClient) Install(ctx context.Context, path, appID string, validate bool) (string, error) {
	return c.appUpdate(ctx, path, appID, validate, nil)
}

func (c *ExecClient) Update(ctx context.Context, path, appID string, validate bool, progress ProgressFunc) (string, error) {
	return c.appUpdate(ctx, path, appID, validate, progress)
}

func (c *ExecClient) Validate(ctx context.Context, path, appID string) (string, error) {
	return c.appUpdate(ctx, path, appID, true, nil)
}

func (c *ExecClient) appUpdate(ctx context.Context, path, appID string, validate bool, progress ProgressFunc) (string, error) {
	args := []string{"+force_install_dir", path, "+login", "anonymous", "+app_update", appID}
	if validate {
		args = append(args, "validate")
	}
	args = append(args, "+quit")
	cmd := exec.CommandContext(ctx, c.binary, args...)
	output := &progressBuffer{limit: 1 << 20, progress: progress}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		return output.String(), fmt.Errorf("steamcmd failed: %w", err)
	}
	return output.String(), nil
}

type progressBuffer struct {
	mu       sync.Mutex
	data     []byte
	limit    int
	progress ProgressFunc
}

func (b *progressBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
	} else {
		b.data = append(b.data, p...)
		if len(b.data) > b.limit {
			copy(b.data, b.data[len(b.data)-b.limit:])
			b.data = b.data[:b.limit]
		}
	}
	if b.progress != nil {
		b.progress(strings.TrimSpace(string(b.data)))
	}
	b.mu.Unlock()
	return len(p), nil
}

func (b *progressBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
