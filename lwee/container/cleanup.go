package container

import (
	"context"
	"fmt"
	"github.com/docker/go-connections/nat"
	"github.com/lefinal/meh"
	"github.com/lefinal/meh/mehlog"
	"go.uber.org/zap"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

type cleanUpper interface {
	start(ctx context.Context) error
	stop()
	registerContainer(containerName string)
}

// nopCleanUpper is a cleanUpper that does nothing. Used for when cleanup is
// disabled.
type nopCleanUpper struct {
}

func newNopCleanUpper() nopCleanUpper {
	return nopCleanUpper{}
}

func (c nopCleanUpper) start(_ context.Context) error {
	return nil
}

func (c nopCleanUpper) stop() {
}

func (c nopCleanUpper) registerContainer(_ string) {
}

type clientCleanUpper struct {
	logger *zap.Logger
	client engineClient
	conn   net.Conn
	m      sync.Mutex
}

func newCleanUpper(logger *zap.Logger, client engineClient) *clientCleanUpper {
	return &clientCleanUpper{
		logger: logger,
		client: client,
	}
}

// start
func (c *clientCleanUpper) start(ctx context.Context) error {
	const retries = 10
	const retryDelay = 100 * time.Millisecond
	const ryukImage = "testcontainers/ryuk:0.5.1"
	const ryukPortNumber = "8080"
	const dockerSocket = "/var/run/docker.sock"

	c.m.Lock()
	defer c.m.Unlock()
	// Assure Ryuk image is available.
	ryukImageExists, err := c.client.imageExists(ctx, ryukImage)
	if err != nil {
		return meh.Wrap(err, "check if ryuk image exists", meh.Details{"image": ryukImage})
	}
	if !ryukImageExists {
		err = c.client.imagePull(ctx, ryukImage)
		if err != nil {
			return meh.Wrap(err, "pull ryuk image", meh.Details{"image": ryukImage})
		}
	}
	// Start Ryuk.
	ryukPort, err := nat.NewPort("tcp", ryukPortNumber)
	if err != nil {
		return meh.NewInternalErrFromErr(err, "new ryuk port", nil)
	}
	ryukContainer, err := c.client.createContainer(ctx, Config{
		ExposedPorts: map[nat.Port]struct{}{
			ryukPort: {},
		},
		Image: ryukImage,
		Env: map[string]string{
			"RYUK_PORT": ryukPortNumber,
		},
		VolumeMounts: []VolumeMount{
			{
				Source: dockerSocket,
				Target: dockerSocket,
			},
		},
		AutoRemove: true,
	})
	if err != nil {
		return meh.Wrap(err, "create ryuk container", nil)
	}
	err = c.client.startContainer(ctx, ryukContainer.id)
	if err != nil {
		return meh.Wrap(err, "start ryuk container", meh.Details{"ryuk_container": ryukContainer})
	}
	ryukIP, err := c.client.containerIP(ctx, ryukContainer.id)
	if err != nil {
		return meh.Wrap(err, "get ryuk container ip", meh.Details{"ryuk_container": ryukContainer})
	}
	ryukConnectAddr := net.JoinHostPort(ryukIP, ryukPortNumber)
	// Connect to Ryuk.
	for try := 0; try < retries; try++ {
		// Try TCP connection to ryuk.
		c.conn, err = net.Dial("tcp", ryukConnectAddr)
		if err == nil {
			break
		}
		// Connection failed.
		err = meh.NewInternalErrFromErr(err, "connect to ryuk", meh.Details{"addr": ryukConnectAddr})
		mehlog.LogToLevel(c.logger, zap.DebugLevel, err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(retryDelay):
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (c *clientCleanUpper) stop() {
	c.m.Lock()
	defer c.m.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *clientCleanUpper) registerContainer(containerName string) {
	if strings.ContainsAny(containerName, "&=") {
		c.logger.Error("cannot register container with ryuk as container name involves invalid symbols",
			zap.String("container_name", containerName))
		return
	}
	c.m.Lock()
	defer c.m.Unlock()
	containerName = strings.TrimPrefix(containerName, "/")
	message := fmt.Sprintf("name=%s\n", containerName)
	_, err := io.Copy(c.conn, strings.NewReader(message))
	if err != nil {
		err = meh.NewInternalErrFromErr(err, "notify ryuk of new container", meh.Details{"message": message})
		mehlog.Log(c.logger, err)
		return
	}
}
