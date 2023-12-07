package external

import (
	"context"
	"crypto/tls"
	"time"

	proxy "github.com/Rocket-Rescue-Node/rescue-proxy/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type RescueProxyAPIClient struct {
	address string
	secure  bool
	conn    *grpc.ClientConn
	client  proxy.ApiClient
}

func NewRescueProxyAPIClient(address string, secure bool) *RescueProxyAPIClient {
	return &RescueProxyAPIClient{address: address, secure: secure}
}

func (c *RescueProxyAPIClient) connect() error {
	var err error

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Try to connect to the Rescue Proxy API using TLS.
	// An empty TLS config will use the system's root CAs.
	tc := credentials.NewTLS(&tls.Config{})
	if c.conn, err = grpc.DialContext(ctx,
		c.address,
		grpc.WithTransportCredentials(tc),
		grpc.WithBlock()); err == nil {

		c.client = proxy.NewApiClient(c.conn)
		return nil
	}

	// If TLS fails, try falling back to insecure gRPC.
	if c.secure {
		return err
	}

	ctx, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()

	if c.conn, err = grpc.DialContext(ctx,
		c.address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock()); err != nil {

		return err
	}

	c.client = proxy.NewApiClient(c.conn)
	return nil
}

func (c *RescueProxyAPIClient) GetRocketPoolNodes() ([][]byte, error) {
	// Connect if not yet connected.
	if c.conn == nil || c.client == nil {
		if err := c.connect(); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.client.GetRocketPoolNodes(ctx, &proxy.RocketPoolNodesRequest{})
	if err != nil {
		return nil, err
	}
	return r.GetNodeIds(), nil
}

func (c *RescueProxyAPIClient) GetWithdrawalAddresses() ([][]byte, error) {
	// Connect if not yet connected.
	if c.conn == nil || c.client == nil {
		if err := c.connect(); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.client.GetSoloValidators(ctx, &proxy.SoloValidatorsRequest{})
	if err != nil {
		return nil, err
	}
	return r.GetWithdrawalAddresses(), nil
}

func (c *RescueProxyAPIClient) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.client = nil
	return err
}
