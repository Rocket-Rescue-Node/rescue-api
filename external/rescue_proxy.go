package external

import (
	"context"
	"time"

	proxy "github.com/Rocket-Pool-Rescue-Node/rescue-proxy/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type RescueProxyAPIClient struct {
	address string
	conn    *grpc.ClientConn
	client  proxy.ApiClient
}

func NewRescueProxyAPIClient(address string) *RescueProxyAPIClient {
	return &RescueProxyAPIClient{address: address}
}

func (c *RescueProxyAPIClient) connect() error {
	var err error
	do := grpc.WithTransportCredentials(insecure.NewCredentials())
	if c.conn, err = grpc.Dial(c.address, do); err != nil {
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

func (c *RescueProxyAPIClient) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.client = nil
	return err
}
